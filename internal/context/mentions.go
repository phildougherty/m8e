package context

import (
	stdctx "context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/kube"
	"github.com/phildougherty/m8e/internal/memory"
	"github.com/phildougherty/m8e/internal/treesitter"
)

// newSchemeWithCRDs builds a runtime.Scheme with all matey CRDs registered so
// the controller-runtime client can list/get MCPTaskScheduler and friends.
func newSchemeWithCRDs() *runtime.Scheme {
	s := runtime.NewScheme()
	// AddToScheme returns nil in practice; we ignore the error to keep the
	// caller path simple, since a failure here is unrecoverable anyway.
	_ = crd.AddToScheme(s)

	return s
}

// MentionType represents different types of mentions
type MentionType string

const (
	MentionTypeFile       MentionType = "file"
	MentionTypeDirectory  MentionType = "directory"
	MentionTypeProblems   MentionType = "problems"
	MentionTypeLogs       MentionType = "logs"
	MentionTypeGitChanges MentionType = "git-changes"
	MentionTypeDefinition MentionType = "definition"
	MentionTypeMemory     MentionType = "memory"
	MentionTypeWorkflow   MentionType = "workflow"
)

// Mention represents a parsed mention from user input
type Mention struct {
	Type       MentionType            `json:"type"`
	Raw        string                 `json:"raw"`
	Path       string                 `json:"path,omitempty"`
	Content    string                 `json:"content"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	Lines      []int                  `json:"lines,omitempty"`
	TokenCount int                    `json:"token_count"`
	Error      string                 `json:"error,omitempty"`
}

// MemoryStore is the minimal slice of the memory.MemoryStore API the mention
// processor needs. Keeping it as an interface lets tests inject a fake without
// requiring a live PostgreSQL connection.
type MemoryStore interface {
	SearchNodes(query string) ([]memory.SearchResult, error)
}

// MentionProcessor handles parsing and resolving mentions
type MentionProcessor struct {
	workDir        string
	fileDiscovery  *FileDiscovery
	contextManager *ContextManager

	// Optional injected dependencies. When nil, the processor falls back to
	// on-demand construction using internal/kube.LoadConfig (k8s clients) or
	// returns an honest error string (memory store). Tests inject fakes here.
	clientset kubernetes.Interface
	k8sClient ctrlclient.Client
	memStore  MemoryStore
	tsParser  *treesitter.TreeSitterParser
	namespace string
}

// MentionConfig configures mention processing behavior
type MentionConfig struct {
	MaxFileSize    int64         `json:"max_file_size"`
	MaxDirFiles    int           `json:"max_dir_files"`
	MaxLogLines    int           `json:"max_log_lines"`
	DefaultLines   int           `json:"default_lines"`
	IncludeHidden  bool          `json:"include_hidden"`
	FollowSymlinks bool          `json:"follow_symlinks"`
	Timeout        time.Duration `json:"timeout"`
}

// Default mention patterns
var mentionPatterns = map[string]*regexp.Regexp{
	"file":        regexp.MustCompile(`@(/[^@\s]+\.[a-zA-Z0-9]+)(?::(\d+)(?:-(\d+))?)?`),
	"directory":   regexp.MustCompile(`@(/[^@\s]+/)(?::(\d+))?`),
	"problems":    regexp.MustCompile(`@problems(?::(\w+))?`),
	"logs":        regexp.MustCompile(`@logs(?::(\w+))?(?::(\d+))?`),
	"git-changes": regexp.MustCompile(`@git-changes(?::(\w+))?`),
	"definition":  regexp.MustCompile(`@def:([^@\s]+)`),
	"memory":      regexp.MustCompile(`@memory(?::([^@\s]+))?`),
	"workflow":    regexp.MustCompile(`@workflow(?::([^@\s]+))?`),
}

// NewMentionProcessor creates a new mention processor
func NewMentionProcessor(workDir string, fileDiscovery *FileDiscovery, contextManager *ContextManager) *MentionProcessor {
	return &MentionProcessor{
		workDir:        workDir,
		fileDiscovery:  fileDiscovery,
		contextManager: contextManager,
		namespace:      defaultNamespace(),
	}
}

// SetKubernetesClients injects the kubernetes clientset and controller-runtime
// client. Callers (or tests) use this to bypass on-demand construction via
// internal/kube.LoadConfig — for example, when wiring fakes or sharing an
// already-constructed client. Passing nil for either argument leaves that
// field untouched so callers can inject one at a time.
func (mp *MentionProcessor) SetKubernetesClients(clientset kubernetes.Interface, k8sClient ctrlclient.Client) {
	if clientset != nil {
		mp.clientset = clientset
	}
	if k8sClient != nil {
		mp.k8sClient = k8sClient
	}
}

// SetMemoryStore injects a memory store implementation. When unset, the
// memory mention returns an honest error instead of fabricating results.
func (mp *MentionProcessor) SetMemoryStore(store MemoryStore) {
	mp.memStore = store
}

// SetTreeSitterParser injects a pre-built tree-sitter parser. When unset, the
// definition mention lazily constructs one with default config.
func (mp *MentionProcessor) SetTreeSitterParser(parser *treesitter.TreeSitterParser) {
	mp.tsParser = parser
}

// SetNamespace overrides the namespace used for k8s queries. Defaults to
// $MATEY_NAMESPACE or "matey", matching the rest of the codebase.
func (mp *MentionProcessor) SetNamespace(namespace string) {
	if namespace != "" {
		mp.namespace = namespace
	}
}

func defaultNamespace() string {
	if ns := os.Getenv("MATEY_NAMESPACE"); ns != "" {
		return ns
	}

	return "matey"
}

// getClientset returns the kubernetes clientset, building one on demand if
// not previously injected via SetKubernetesClients.
func (mp *MentionProcessor) getClientset() (kubernetes.Interface, error) {
	if mp.clientset != nil {
		return mp.clientset, nil
	}
	cfg, err := kube.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("kube.LoadConfig: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes.NewForConfig: %w", err)
	}
	mp.clientset = cs

	return cs, nil
}

// getCtrlClient returns the controller-runtime client, building one on demand
// if not previously injected. The scheme has every matey CRD registered so
// MCPTaskScheduler can be listed without further setup.
func (mp *MentionProcessor) getCtrlClient() (ctrlclient.Client, error) {
	if mp.k8sClient != nil {
		return mp.k8sClient, nil
	}
	cfg, err := kube.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("kube.LoadConfig: %w", err)
	}
	scheme := newSchemeWithCRDs()
	c, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("controller-runtime client: %w", err)
	}
	mp.k8sClient = c

	return c, nil
}

// getTreeSitterParser returns the tree-sitter parser, building one on demand
// if not previously injected. We use lazy language loading so the parser is
// cheap to construct even when no @def mention is ever issued.
func (mp *MentionProcessor) getTreeSitterParser() (*treesitter.TreeSitterParser, error) {
	if mp.tsParser != nil {
		return mp.tsParser, nil
	}
	p, err := treesitter.NewTreeSitterParser(treesitter.ParserConfig{LazyLoad: true})
	if err != nil {
		return nil, err
	}
	mp.tsParser = p

	return p, nil
}

// ParseMentions parses all mentions from a text string
func (mp *MentionProcessor) ParseMentions(text string) ([]Mention, error) {
	var mentions []Mention

	// Process each mention type
	for mentionType, pattern := range mentionPatterns {
		matches := pattern.FindAllStringSubmatch(text, -1)

		for _, match := range matches {
			mention, err := mp.processMention(MentionType(mentionType), match)
			if err != nil {
				mention.Error = err.Error()
			}
			mentions = append(mentions, mention)
		}
	}

	// Sort mentions by position in text
	sort.Slice(mentions, func(i, j int) bool {
		return strings.Index(text, mentions[i].Raw) < strings.Index(text, mentions[j].Raw)
	})

	return mentions, nil
}

// ProcessMention processes a single mention and returns its content
func (mp *MentionProcessor) ProcessMention(mention Mention) (Mention, error) {
	config := MentionConfig{
		MaxFileSize:  1024 * 1024, // 1MB
		MaxDirFiles:  50,
		MaxLogLines:  100,
		DefaultLines: 20,
		Timeout:      10 * time.Second,
	}

	switch mention.Type {
	case MentionTypeFile:
		return mp.processFileMention(mention, config)
	case MentionTypeDirectory:
		return mp.processDirectoryMention(mention, config)
	case MentionTypeProblems:
		return mp.processProblemsMention(mention, config)
	case MentionTypeLogs:
		return mp.processLogsMention(mention, config)
	case MentionTypeGitChanges:
		return mp.processGitChangesMention(mention, config)
	case MentionTypeDefinition:
		return mp.processDefinitionMention(mention, config)
	case MentionTypeMemory:
		return mp.processMemoryMention(mention, config)
	case MentionTypeWorkflow:
		return mp.processWorkflowMention(mention, config)
	default:
		return mention, fmt.Errorf("unknown mention type: %s", mention.Type)
	}
}

// ExpandText replaces all mentions in text with their resolved content
func (mp *MentionProcessor) ExpandText(text string) (string, []Mention, error) {
	mentions, err := mp.ParseMentions(text)
	if err != nil {
		return text, nil, err
	}

	expanded := text
	var processedMentions []Mention

	for _, mention := range mentions {
		processed, err := mp.ProcessMention(mention)
		if err != nil {
			processed.Error = err.Error()
		}

		// Replace mention with content
		if processed.Content != "" {
			replacement := fmt.Sprintf("\n\n--- %s ---\n%s\n--- End %s ---\n",
				processed.Raw, processed.Content, processed.Raw)
			expanded = strings.Replace(expanded, processed.Raw, replacement, 1)
		}

		processedMentions = append(processedMentions, processed)
	}

	return expanded, processedMentions, nil
}

// Private methods

func (mp *MentionProcessor) processMention(mentionType MentionType, match []string) (Mention, error) {
	mention := Mention{
		Type: mentionType,
		Raw:  match[0],
	}

	switch mentionType {
	case MentionTypeFile:
		mention.Path = match[1]
		if len(match) > 2 && match[2] != "" {
			if start, err := strconv.Atoi(match[2]); err == nil {
				mention.Lines = []int{start}
				if len(match) > 3 && match[3] != "" {
					if end, err := strconv.Atoi(match[3]); err == nil {
						mention.Lines = []int{start, end}
					}
				}
			}
		}

	case MentionTypeDirectory:
		mention.Path = match[1]
		if len(match) > 2 && match[2] != "" {
			if limit, err := strconv.Atoi(match[2]); err == nil {
				mention.Metadata = map[string]interface{}{"limit": limit}
			}
		}

	case MentionTypeProblems:
		if len(match) > 1 && match[1] != "" {
			mention.Metadata = map[string]interface{}{"namespace": match[1]}
		}

	case MentionTypeLogs:
		if len(match) > 1 && match[1] != "" {
			mention.Metadata = map[string]interface{}{"service": match[1]}
		}
		if len(match) > 2 && match[2] != "" {
			if lines, err := strconv.Atoi(match[2]); err == nil {
				mention.Metadata = map[string]interface{}{"lines": lines}
			}
		}

	case MentionTypeGitChanges:
		if len(match) > 1 && match[1] != "" {
			mention.Metadata = map[string]interface{}{"branch": match[1]}
		}

	case MentionTypeDefinition:
		mention.Path = match[1]

	case MentionTypeMemory:
		if len(match) > 1 && match[1] != "" {
			mention.Path = match[1]
		}

	case MentionTypeWorkflow:
		if len(match) > 1 && match[1] != "" {
			mention.Path = match[1]
		}
	}

	return mention, nil
}

func (mp *MentionProcessor) processFileMention(mention Mention, config MentionConfig) (Mention, error) {
	// Resolve path relative to working directory
	path := mention.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(mp.workDir, path)
	}

	// Check if file exists
	info, err := os.Stat(path)
	if err != nil {
		return mention, fmt.Errorf("file not found: %s", mention.Path)
	}

	// Check file size
	if info.Size() > config.MaxFileSize {
		return mention, fmt.Errorf("file too large: %d bytes (max: %d)", info.Size(), config.MaxFileSize)
	}

	// Read file content
	content, err := os.ReadFile(path)
	if err != nil {
		return mention, fmt.Errorf("failed to read file: %w", err)
	}

	contentStr := string(content)

	// Extract specific lines if requested
	if len(mention.Lines) > 0 {
		lines := strings.Split(contentStr, "\n")
		start := mention.Lines[0] - 1 // Convert to 0-based
		end := start + 1

		if len(mention.Lines) > 1 {
			end = mention.Lines[1]
		}

		if start < 0 {
			start = 0
		}
		if end > len(lines) {
			end = len(lines)
		}
		if start >= len(lines) {
			return mention, fmt.Errorf("line number %d exceeds file length", mention.Lines[0])
		}

		selectedLines := lines[start:end]

		// Add line number prefix
		var numberedLines []string
		for i, line := range selectedLines {
			numberedLines = append(numberedLines, fmt.Sprintf("%d: %s", start+i+1, line))
		}
		contentStr = strings.Join(numberedLines, "\n")
	}

	mention.Content = contentStr
	mention.TokenCount = len(contentStr) / 4 // Rough estimate
	mention.Metadata = map[string]interface{}{
		"size":     info.Size(),
		"mod_time": info.ModTime(),
		"path":     path,
	}

	// Add to context manager
	if mp.contextManager != nil {
		if err := mp.contextManager.AddContext(ContextTypeFile, path, contentStr, mention.Metadata); err != nil {
			// Log error but don't fail the mention processing
			fmt.Printf("Warning: Failed to add file context: %v\n", err)
		}
	}

	return mention, nil
}

func (mp *MentionProcessor) processDirectoryMention(mention Mention, config MentionConfig) (Mention, error) {
	path := mention.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(mp.workDir, path)
	}

	// Check if directory exists
	info, err := os.Stat(path)
	if err != nil {
		return mention, fmt.Errorf("directory not found: %s", mention.Path)
	}
	if !info.IsDir() {
		return mention, fmt.Errorf("path is not a directory: %s", mention.Path)
	}

	// Get directory listing
	entries, err := os.ReadDir(path)
	if err != nil {
		return mention, fmt.Errorf("failed to read directory: %w", err)
	}

	// Apply limit
	limit := config.MaxDirFiles
	if mention.Metadata != nil {
		if l, ok := mention.Metadata["limit"].(int); ok {
			limit = l
		}
	}

	var listing []string
	count := 0

	for _, entry := range entries {
		if count >= limit {
			listing = append(listing, "... (truncated)")
			break
		}

		// Skip hidden files unless configured
		if strings.HasPrefix(entry.Name(), ".") && !config.IncludeHidden {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		var line string
		if entry.IsDir() {
			line = fmt.Sprintf("%s/", entry.Name())
		} else {
			line = fmt.Sprintf("%s (%d bytes)", entry.Name(), info.Size())
		}

		listing = append(listing, line)
		count++
	}

	mention.Content = strings.Join(listing, "\n")
	mention.TokenCount = len(mention.Content) / 4
	mention.Metadata = map[string]interface{}{
		"path":        path,
		"total_files": len(entries),
		"shown_files": count,
	}

	return mention, nil
}

func (mp *MentionProcessor) processProblemsMention(mention Mention, config MentionConfig) (Mention, error) {
	namespace := mp.namespace
	if mention.Metadata != nil {
		if ns, ok := mention.Metadata["namespace"].(string); ok && ns != "" {
			namespace = ns
		}
	}

	clientset, err := mp.getClientset()
	if err != nil {
		mention.Content = fmt.Sprintf("@problems: failed to build k8s client: %v; check that matey has cluster access", err)
		mention.TokenCount = len(mention.Content) / 4
		mention.Metadata = map[string]interface{}{"namespace": namespace, "error": err.Error()}

		return mention, err
	}

	ctx, cancel := stdctx.WithTimeout(stdctx.Background(), 10*time.Second)
	defer cancel()

	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		mention.Content = fmt.Sprintf("@problems: failed to query k8s API: %v; check that matey has cluster access", err)
		mention.TokenCount = len(mention.Content) / 4
		mention.Metadata = map[string]interface{}{"namespace": namespace, "error": err.Error()}

		return mention, err
	}

	var problems []string
	for _, pod := range pods.Items {
		if reason, msg, ok := podProblemSummary(pod); ok {
			problems = append(problems, fmt.Sprintf("[%s/%s] %s: %s", pod.Namespace, pod.Name, reason, msg))
		}
	}

	var content strings.Builder
	content.WriteString(fmt.Sprintf("Kubernetes diagnostics for namespace: %s\n", namespace))
	content.WriteString(fmt.Sprintf("Scanned %d pod(s) at %s\n\n", len(pods.Items), time.Now().Format(time.RFC3339)))
	if len(problems) == 0 {
		content.WriteString("No problem pods detected (all pods Running and Ready).\n")
	} else {
		content.WriteString(fmt.Sprintf("%d problem pod(s):\n", len(problems)))
		for _, p := range problems {
			content.WriteString("  - ")
			content.WriteString(p)
			content.WriteString("\n")
		}
	}

	mention.Content = content.String()
	mention.TokenCount = len(mention.Content) / 4
	mention.Metadata = map[string]interface{}{
		"namespace":     namespace,
		"timestamp":     time.Now(),
		"pod_count":     len(pods.Items),
		"problem_count": len(problems),
	}

	if mp.contextManager != nil {
		if err := mp.contextManager.AddContext(ContextTypeDiagnostic, "", mention.Content, mention.Metadata); err != nil {
			fmt.Printf("Warning: Failed to add diagnostic context: %v\n", err)
		}
	}

	return mention, nil
}

// podProblemSummary inspects a pod and, if it is in a non-healthy state,
// returns a short reason and message. The "ok" flag is true when the pod
// counts as a problem. Cases covered: non-Running phase, container waiting
// reasons like CrashLoopBackOff / ImagePullBackOff, and explicit
// ContainerStatus.State.Waiting/Terminated payloads. A Running pod with all
// containers Ready returns ok=false.
func podProblemSummary(pod corev1.Pod) (reason, message string, ok bool) {
	switch pod.Status.Phase {
	case corev1.PodFailed:
		return "Failed", pod.Status.Message, true
	case corev1.PodPending:
		// A Pending pod is only a problem if a container is stuck waiting; if
		// it's just being scheduled briefly we let it pass.
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" && cs.State.Waiting.Reason != "ContainerCreating" {
				return cs.State.Waiting.Reason, fmt.Sprintf("container %s: %s", cs.Name, cs.State.Waiting.Message), true
			}
		}
		for _, cs := range pod.Status.InitContainerStatuses {
			if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" && cs.State.Waiting.Reason != "PodInitializing" {
				return cs.State.Waiting.Reason, fmt.Sprintf("init-container %s: %s", cs.Name, cs.State.Waiting.Message), true
			}
		}
		// Bare Pending with no informative container state: still surface it.
		return "Pending", "pod is pending", true
	case corev1.PodRunning:
		// Running but a container is in CrashLoopBackOff / restart trouble.
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
				return cs.State.Waiting.Reason, fmt.Sprintf("container %s: %s", cs.Name, cs.State.Waiting.Message), true
			}
			if !cs.Ready {
				return "NotReady", fmt.Sprintf("container %s not ready", cs.Name), true
			}
		}
	case corev1.PodUnknown:
		return "Unknown", pod.Status.Message, true
	}

	return "", "", false
}

func (mp *MentionProcessor) processLogsMention(mention Mention, config MentionConfig) (Mention, error) {
	service := ""
	lines := config.MaxLogLines
	if lines == 0 {
		lines = 100
	}
	namespace := mp.namespace

	if mention.Metadata != nil {
		if s, ok := mention.Metadata["service"].(string); ok {
			service = s
		}
		if l, ok := mention.Metadata["lines"].(int); ok && l > 0 {
			lines = l
		}
		if ns, ok := mention.Metadata["namespace"].(string); ok && ns != "" {
			namespace = ns
		}
	}

	if service == "" {
		mention.Content = "@logs: no service specified; use @logs:<service-name>"
		mention.TokenCount = len(mention.Content) / 4
		mention.Metadata = map[string]interface{}{"service": service, "namespace": namespace}

		return mention, fmt.Errorf("@logs: service name required")
	}

	clientset, err := mp.getClientset()
	if err != nil {
		mention.Content = fmt.Sprintf("@logs: failed to build k8s client: %v; check that matey has cluster access", err)
		mention.TokenCount = len(mention.Content) / 4
		mention.Metadata = map[string]interface{}{"service": service, "namespace": namespace, "error": err.Error()}

		return mention, err
	}

	// Streaming a tail may take longer than a list — give it 30s.
	ctx, cancel := stdctx.WithTimeout(stdctx.Background(), 30*time.Second)
	defer cancel()

	pod, err := mp.findReadyPodForService(ctx, clientset, namespace, service)
	if err != nil {
		mention.Content = fmt.Sprintf("@logs: %v", err)
		mention.TokenCount = len(mention.Content) / 4
		mention.Metadata = map[string]interface{}{"service": service, "namespace": namespace, "error": err.Error()}

		return mention, err
	}

	tail := int64(lines)
	req := clientset.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		TailLines: &tail,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		mention.Content = fmt.Sprintf("@logs: failed to open log stream for %s/%s: %v", namespace, pod.Name, err)
		mention.TokenCount = len(mention.Content) / 4
		mention.Metadata = map[string]interface{}{"service": service, "namespace": namespace, "pod": pod.Name, "error": err.Error()}

		return mention, err
	}
	defer func() {
		if cerr := stream.Close(); cerr != nil {
			fmt.Printf("Warning: failed to close log stream: %v\n", cerr)
		}
	}()

	logBytes, err := io.ReadAll(stream)
	if err != nil {
		mention.Content = fmt.Sprintf("@logs: failed to read log stream for %s/%s: %v", namespace, pod.Name, err)
		mention.TokenCount = len(mention.Content) / 4
		mention.Metadata = map[string]interface{}{"service": service, "namespace": namespace, "pod": pod.Name, "error": err.Error()}

		return mention, err
	}

	var content strings.Builder
	content.WriteString(fmt.Sprintf("Logs for %s/%s (last %d lines, service=%s):\n", namespace, pod.Name, lines, service))
	content.WriteString(fmt.Sprintf("Fetched at: %s\n\n", time.Now().Format(time.RFC3339)))
	content.Write(logBytes)

	mention.Content = content.String()
	mention.TokenCount = len(mention.Content) / 4
	mention.Metadata = map[string]interface{}{
		"service":   service,
		"namespace": namespace,
		"pod":       pod.Name,
		"lines":     lines,
		"timestamp": time.Now(),
	}

	if mp.contextManager != nil {
		if err := mp.contextManager.AddContext(ContextTypeLog, "", mention.Content, mention.Metadata); err != nil {
			fmt.Printf("Warning: Failed to add log context: %v\n", err)
		}
	}

	return mention, nil
}

// findReadyPodForService resolves a service name to a single pod whose logs
// we can stream. Resolution order: pods labeled app=<service>, then pods
// labeled app.kubernetes.io/name=<service>, then any pod whose name contains
// <service> as a substring. Within each list a Running+Ready pod is preferred;
// if none, the first pod is returned so callers still get diagnostic output.
func (mp *MentionProcessor) findReadyPodForService(ctx stdctx.Context, clientset kubernetes.Interface, namespace, service string) (*corev1.Pod, error) {
	selectors := []string{
		fmt.Sprintf("app=%s", service),
		fmt.Sprintf("app.kubernetes.io/name=%s", service),
	}
	for _, sel := range selectors {
		pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: sel})
		if err != nil {
			return nil, fmt.Errorf("failed to list pods with selector %q: %w", sel, err)
		}
		if len(pods.Items) == 0 {
			continue
		}
		if p := pickReadyPod(pods.Items); p != nil {
			return p, nil
		}
	}

	// Fall back to name substring match.
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods in namespace %s: %w", namespace, err)
	}
	var matches []corev1.Pod
	for _, p := range pods.Items {
		if strings.Contains(p.Name, service) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no pod found matching service %q in namespace %s", service, namespace)
	}
	if p := pickReadyPod(matches); p != nil {
		return p, nil
	}

	return &matches[0], nil
}

func pickReadyPod(pods []corev1.Pod) *corev1.Pod {
	for i := range pods {
		p := pods[i]
		if p.Status.Phase != corev1.PodRunning {
			continue
		}
		ready := true
		for _, cs := range p.Status.ContainerStatuses {
			if !cs.Ready {
				ready = false
				break
			}
		}
		if ready {
			return &p
		}
	}
	if len(pods) > 0 {
		return &pods[0]
	}

	return nil
}

func (mp *MentionProcessor) processGitChangesMention(mention Mention, config MentionConfig) (Mention, error) {
	branch := "current"
	if mention.Metadata != nil {
		if b, ok := mention.Metadata["branch"].(string); ok {
			branch = b
		}
	}

	// Use file discovery to get git status
	if mp.fileDiscovery != nil {
		content := fmt.Sprintf("Git changes for branch: %s\n", branch)
		content += fmt.Sprintf("Repository: %s\n", mp.fileDiscovery.GetRepoRoot())
		content += fmt.Sprintf("Current branch: %s\n\n", mp.fileDiscovery.GetGitBranch())

		// Get recent commits
		if commits, err := mp.fileDiscovery.GetRecentCommits(5); err == nil {
			content += "Recent commits:\n"
			for _, commit := range commits {
				content += fmt.Sprintf("  %s - %s (%s)\n",
					commit["hash"], commit["message"], commit["author"])
			}
		}

		mention.Content = content
		mention.TokenCount = len(content) / 4
		mention.Metadata = map[string]interface{}{
			"branch": branch,
			"repo":   mp.fileDiscovery.GetRepoRoot(),
		}

		if mp.contextManager != nil {
			if err := mp.contextManager.AddContext(ContextTypeGit, "", content, mention.Metadata); err != nil {
				// Log error but don't fail the mention processing
				fmt.Printf("Warning: Failed to add git context: %v\n", err)
			}
		}
	}

	return mention, nil
}

func (mp *MentionProcessor) processDefinitionMention(mention Mention, config MentionConfig) (Mention, error) {
	name := strings.TrimSpace(mention.Path)
	if name == "" {
		mention.Content = "@def: no symbol name provided; use @def:Name"
		mention.TokenCount = len(mention.Content) / 4

		return mention, fmt.Errorf("@def: name required")
	}

	parser, err := mp.getTreeSitterParser()
	if err != nil {
		mention.Content = fmt.Sprintf("@def: failed to initialize tree-sitter parser: %v", err)
		mention.TokenCount = len(mention.Content) / 4
		mention.Metadata = map[string]interface{}{"query": name, "error": err.Error()}

		return mention, err
	}
	extractor := treesitter.NewDefinitionExtractor(parser)

	root := mp.definitionWorkspaceRoot()

	ctx, cancel := stdctx.WithTimeout(stdctx.Background(), 30*time.Second)
	defer cancel()

	// Walk the workspace looking for source files. Tree-sitter supports Go,
	// Python, JavaScript, and Rust; anything else we silently skip.
	var matches []treesitter.Definition
	var visited int
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip unreadable entries instead of aborting the whole walk.
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			// Prune common noisy or massive directories that would dominate the walk.
			base := d.Name()
			if base != "." && (strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor" || base == "dist" || base == "build") {
				return filepath.SkipDir
			}

			return nil
		}
		lang := parser.DetectLanguage(path)
		if lang == treesitter.LanguageUnknown || !parser.IsLanguageSupported(lang) {
			return nil
		}
		// Read directly; the parser package's own readFile is a stub.
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		visited++
		res, perr := parser.ParseContent(string(data), lang, path)
		if perr != nil {
			return nil
		}
		defs, derr := extractor.ExtractDefinitions(res)
		if derr != nil {
			return nil
		}
		for _, def := range defs {
			if def.Name == name {
				matches = append(matches, def)
			}
		}

		return nil
	})

	if walkErr != nil && !errorsIs(walkErr, stdctx.DeadlineExceeded) {
		mention.Content = fmt.Sprintf("@def: workspace walk failed under %s: %v", root, walkErr)
		mention.TokenCount = len(mention.Content) / 4
		mention.Metadata = map[string]interface{}{"query": name, "root": root, "error": walkErr.Error()}

		return mention, walkErr
	}

	var content strings.Builder
	content.WriteString(fmt.Sprintf("Definitions for %q (workspace=%s, files scanned=%d):\n", name, root, visited))
	if len(matches) == 0 {
		content.WriteString("  (no matches found)\n")
	} else {
		for _, m := range matches {
			sig := strings.TrimSpace(strings.Split(m.Signature, "\n")[0])
			content.WriteString(fmt.Sprintf("  %s:%d: [%s] %s\n", m.FilePath, m.StartLine, m.Type, sig))
		}
	}

	mention.Content = content.String()
	mention.TokenCount = len(mention.Content) / 4
	mention.Metadata = map[string]interface{}{
		"query":         name,
		"root":          root,
		"files_scanned": visited,
		"match_count":   len(matches),
	}

	if mp.contextManager != nil {
		if err := mp.contextManager.AddContext(ContextTypeDefinition, "", mention.Content, mention.Metadata); err != nil {
			fmt.Printf("Warning: Failed to add definition context: %v\n", err)
		}
	}

	return mention, nil
}

// errorsIs is a small wrapper so we do not have to drag the "errors" package
// import into a file already crowded with std-lib aliases. It mirrors
// errors.Is semantics for the small set of sentinel errors we care about.
func errorsIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}

	return false
}

func (mp *MentionProcessor) definitionWorkspaceRoot() string {
	if mp.fileDiscovery != nil {
		if r := mp.fileDiscovery.GetRepoRoot(); r != "" {
			return r
		}
	}
	if mp.workDir != "" {
		return mp.workDir
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}

	return "."
}

func (mp *MentionProcessor) processMemoryMention(mention Mention, config MentionConfig) (Mention, error) {
	query := strings.TrimSpace(mention.Path)
	if query == "" {
		mention.Content = "@memory: no query provided; use @memory:<search-term>"
		mention.TokenCount = len(mention.Content) / 4
		mention.Metadata = map[string]interface{}{"query": query}

		return mention, fmt.Errorf("@memory: query required")
	}

	if mp.memStore == nil {
		// Honest error: we do not have a wired memory store. The caller (e.g.
		// the TermChat constructor in internal/mcp/server_integration.go) is
		// responsible for injecting one via SetMemoryStore. The README's
		// "configure DATABASE_URL" hint applies.
		mention.Content = "@memory: memory store is not configured for this mention processor; inject one via MentionProcessor.SetMemoryStore"
		mention.TokenCount = len(mention.Content) / 4
		mention.Metadata = map[string]interface{}{"query": query, "error": "memory store not configured"}

		return mention, fmt.Errorf("@memory: store not configured")
	}

	results, err := mp.memStore.SearchNodes(query)
	if err != nil {
		mention.Content = fmt.Sprintf("@memory: search failed for query %q: %v", query, err)
		mention.TokenCount = len(mention.Content) / 4
		mention.Metadata = map[string]interface{}{"query": query, "error": err.Error()}

		return mention, err
	}

	var content strings.Builder
	content.WriteString(fmt.Sprintf("Memory search for %q (%d result(s)):\n", query, len(results)))
	if len(results) == 0 {
		content.WriteString("  (no entities matched)\n")
	} else {
		for _, r := range results {
			content.WriteString(fmt.Sprintf("\n- %s [%s] (relevance=%.3f)\n", r.Entity.Name, r.Entity.EntityType, r.Relevance))
			if len(r.Matches) > 0 {
				content.WriteString("  matched observations:\n")
				for _, m := range r.Matches {
					content.WriteString("    * ")
					content.WriteString(strings.TrimSpace(m))
					content.WriteString("\n")
				}
			}
			if len(r.Entity.Observations) > 0 && len(r.Matches) == 0 {
				content.WriteString("  observations:\n")
				for _, o := range r.Entity.Observations {
					content.WriteString("    * ")
					content.WriteString(strings.TrimSpace(o))
					content.WriteString("\n")
				}
			}
		}
	}

	mention.Content = content.String()
	mention.TokenCount = len(mention.Content) / 4
	mention.Metadata = map[string]interface{}{
		"query":        query,
		"result_count": len(results),
	}

	if mp.contextManager != nil {
		if err := mp.contextManager.AddContext(ContextTypeDiagnostic, "", mention.Content, mention.Metadata); err != nil {
			fmt.Printf("Warning: Failed to add memory context: %v\n", err)
		}
	}

	return mention, nil
}

func (mp *MentionProcessor) processWorkflowMention(mention Mention, config MentionConfig) (Mention, error) {
	filterName := strings.TrimSpace(mention.Path)
	namespace := mp.namespace

	k8sClient, err := mp.getCtrlClient()
	if err != nil {
		mention.Content = fmt.Sprintf("@workflow: failed to build k8s client: %v; check that matey has cluster access", err)
		mention.TokenCount = len(mention.Content) / 4
		mention.Metadata = map[string]interface{}{"workflow": filterName, "namespace": namespace, "error": err.Error()}

		return mention, err
	}

	ctx, cancel := stdctx.WithTimeout(stdctx.Background(), 10*time.Second)
	defer cancel()

	var schedulers crd.MCPTaskSchedulerList
	if err := k8sClient.List(ctx, &schedulers, ctrlclient.InNamespace(namespace)); err != nil {
		mention.Content = fmt.Sprintf("@workflow: failed to list MCPTaskScheduler in %s: %v", namespace, err)
		mention.TokenCount = len(mention.Content) / 4
		mention.Metadata = map[string]interface{}{"workflow": filterName, "namespace": namespace, "error": err.Error()}

		return mention, err
	}

	type wfRow struct {
		Workflow string
		Schedule string
		Enabled  bool
		LastRun  *crd.WorkflowExecution
	}

	var rows []wfRow
	for _, sched := range schedulers.Items {
		// Index the most recent execution per workflow name. Status.WorkflowExecutions
		// is documented as the most recent 10 per workflow.
		latest := map[string]*crd.WorkflowExecution{}
		for i := range sched.Status.WorkflowExecutions {
			exe := &sched.Status.WorkflowExecutions[i]
			cur, ok := latest[exe.WorkflowName]
			if !ok || exe.StartTime.After(cur.StartTime) {
				latest[exe.WorkflowName] = exe
			}
		}

		for _, wf := range sched.Spec.Workflows {
			if filterName != "" && wf.Name != filterName {
				continue
			}
			rows = append(rows, wfRow{
				Workflow: wf.Name,
				Schedule: wf.Schedule,
				Enabled:  wf.Enabled,
				LastRun:  latest[wf.Name],
			})
		}
	}

	var content strings.Builder
	if filterName != "" {
		content.WriteString(fmt.Sprintf("Workflow status for %q in namespace %s:\n", filterName, namespace))
	} else {
		content.WriteString(fmt.Sprintf("Workflow status in namespace %s:\n", namespace))
	}
	if len(rows) == 0 {
		content.WriteString("  (no matching workflows defined)\n")
	} else {
		for _, r := range rows {
			content.WriteString(fmt.Sprintf("  %s: schedule=%q enabled=%t", r.Workflow, r.Schedule, r.Enabled))
			if r.LastRun == nil {
				content.WriteString(" lastRun=<never>\n")
				continue
			}
			content.WriteString(fmt.Sprintf(" lastRun(time=%s, status=%s",
				r.LastRun.StartTime.Format(time.RFC3339), r.LastRun.Phase))
			if r.LastRun.Duration != nil {
				content.WriteString(fmt.Sprintf(", duration=%s", r.LastRun.Duration.String()))
			} else if r.LastRun.EndTime != nil {
				content.WriteString(fmt.Sprintf(", duration=%s", r.LastRun.EndTime.Sub(r.LastRun.StartTime).String()))
			}
			content.WriteString(")\n")
		}
	}

	mention.Content = content.String()
	mention.TokenCount = len(mention.Content) / 4
	mention.Metadata = map[string]interface{}{
		"workflow":      filterName,
		"namespace":     namespace,
		"workflow_rows": len(rows),
	}

	if mp.contextManager != nil {
		if err := mp.contextManager.AddContext(ContextTypeDiagnostic, "", mention.Content, mention.Metadata); err != nil {
			fmt.Printf("Warning: Failed to add workflow context: %v\n", err)
		}
	}

	return mention, nil
}

// GetSupportedMentions returns information about all supported mention types
func (mp *MentionProcessor) GetSupportedMentions() map[string]string {
	return map[string]string{
		"@/path/file.ext":       "Include file content",
		"@/path/file.ext:10":    "Include specific line",
		"@/path/file.ext:10-20": "Include line range",
		"@/path/folder/":        "List directory contents",
		"@/path/folder/:10":     "List directory (limit 10 files)",
		"@problems":             "Show Kubernetes diagnostics",
		"@problems:namespace":   "Show diagnostics for specific namespace",
		"@logs":                 "Show recent pod logs",
		"@logs:service":         "Show logs for specific service",
		"@logs:service:100":     "Show specific number of log lines",
		"@git-changes":          "Show git status and recent commits",
		"@git-changes:branch":   "Show changes for specific branch",
		"@def:functionName":     "Find code definitions",
		"@memory":               "Query memory service",
		"@memory:query":         "Search memories with query",
		"@workflow":             "Show workflow status",
		"@workflow:name":        "Show specific workflow status",
	}
}
