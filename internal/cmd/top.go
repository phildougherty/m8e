// internal/cmd/top.go
package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	
	"github.com/phildougherty/m8e/internal/compose"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Color constants matching chat UI theme
var (
	ArmyGreen   = lipgloss.Color("58")   // #5f5f00 - darker army green
	LightGreen  = lipgloss.Color("64")   // #5f8700 - lighter army green  
	Brown       = lipgloss.Color("94")   // #875f00 - brown
	Yellow      = lipgloss.Color("226")  // #ffff00 - bright yellow
	GoldYellow  = lipgloss.Color("220")  // #ffd700 - gold yellow
	Red         = lipgloss.Color("196")  // #ff0000 - red
	Tan         = lipgloss.Color("180")  // #d7af87 - tan
)

// Styles for the TUI with consistent chat UI theme
var (
	titleStyle = lipgloss.NewStyle().
		Foreground(ArmyGreen).
		Bold(true).
		Margin(0, 1)

	headerStyle = lipgloss.NewStyle().
		Foreground(Brown).
		Bold(true).
		Underline(true)

	statusRunningStyle = lipgloss.NewStyle().
		Foreground(LightGreen).
		Bold(true)

	statusPendingStyle = lipgloss.NewStyle().
		Foreground(GoldYellow).
		Bold(true)

	statusErrorStyle = lipgloss.NewStyle().
		Foreground(Red).
		Bold(true)

	statusUnknownStyle = lipgloss.NewStyle().
		Foreground(Tan).
		Bold(true)

	tableStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Brown).
		Padding(1, 2)

	helpStyle = lipgloss.NewStyle().
		Foreground(Yellow).
		Italic(true)

	statusBarStyle = lipgloss.NewStyle().
		Foreground(GoldYellow).
		Bold(true)

	separatorStyle = lipgloss.NewStyle().
		Foreground(Brown)
)

// ServerInfo contains detailed information about a server
type ServerInfo struct {
	Name        string
	Status      string
	Type        string
	Restarts    int32
	Age         time.Duration
	CPU         string
	Memory      string
	Image       string
	Protocol    string
	Port        int32
	Replicas    string
	Ready       bool
	LastUpdated time.Time
}

// TopModel represents the state of the top TUI
type TopModel struct {
	servers       []ServerInfo
	lastUpdate    time.Time
	refreshRate   time.Duration
	composer      *compose.K8sComposer
	k8sClient     kubernetes.Interface
	width         int
	height        int
	sortBy        string
	sortDesc      bool
	showHelp      bool
	err           error
}

// Messages for the TUI
type tickMsg time.Time
type serversMsg []ServerInfo
type errMsg error

func NewTopCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "top",
		Short: "Display a live view of MCP servers with detailed information",
		Long: `Display a live view of MCP servers with enhanced visual monitoring including:
- Real-time status updates with color coding
- Resource usage tracking (CPU, Memory)
- Restart counts and age information
- Container and protocol information
- Network configuration details
- Interactive sorting by multiple criteria
- Enhanced color theme matching Matey chat UI

Enhanced Controls:
- q/Ctrl+C: Quit                    - F5/Ctrl+R: Force refresh
- h/?: Toggle help display          - Space: Reverse sort order
- n: Sort by name                   - s: Sort by status
- t: Sort by type                   - r: Sort by restarts
- a: Sort by age                    - p: Sort by protocol
- c: Sort by CPU usage              - m: Sort by memory usage

Features:
- Color-coded status indicators     - Type-based server coloring
- Age-based time coloring          - Protocol-specific highlighting
- Alternating row backgrounds      - Enhanced error handling`,
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("file")
			refreshRate, _ := cmd.Flags().GetDuration("refresh")
			
			return runTop(file, refreshRate)
		},
	}

	cmd.Flags().Duration("refresh", 2*time.Second, "Refresh interval")
	cmd.Flags().StringP("sort", "s", "name", "Sort by field (name, status, restarts, age)")
	cmd.Flags().Bool("desc", false, "Sort in descending order")

	return cmd
}

func runTop(configFile string, refreshRate time.Duration) error {
	// Create composer
	composer, err := compose.NewK8sComposer(configFile, "default")
	if err != nil {
		return fmt.Errorf("failed to create composer: %w", err)
	}

	// Create Kubernetes client
	k8sClient, err := createK8sClientForTop()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Initialize model
	model := TopModel{
		composer:    composer,
		k8sClient:   k8sClient,
		refreshRate: refreshRate,
		sortBy:      "name",
		sortDesc:    false,
		showHelp:    false,
	}

	// Start the TUI
	program := tea.NewProgram(&model, tea.WithAltScreen())
	_, err = program.Run()
	if err != nil {
		// If TUI fails, provide a helpful error message
		return fmt.Errorf("failed to start TUI interface: %w\n\nNote: 'matey top' requires a terminal (TTY) to run. Use 'matey ps' for non-interactive status.", err)
	}
	return nil
}

func createK8sClientForTop() (kubernetes.Interface, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return clientset, nil
}

// Init initializes the model
func (m *TopModel) Init() tea.Cmd {
	return tea.Batch(
		tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg {
			return tickMsg(t)
		}),
		m.fetchServers(),
	)
}

// Update handles messages
func (m *TopModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "s":
			m.sortBy = "status"
			m.sortServers()
			return m, nil
		case "n":
			m.sortBy = "name"
			m.sortServers()
			return m, nil
		case "r":
			m.sortBy = "restarts"
			m.sortServers()
			return m, nil
		case "a":
			m.sortBy = "age"
			m.sortServers()
			return m, nil
		case "t":
			m.sortBy = "type"
			m.sortServers()
			return m, nil
		case "p":
			m.sortBy = "protocol"
			m.sortServers()
			return m, nil
		case "m":
			m.sortBy = "memory"
			m.sortServers()
			return m, nil
		case "c":
			m.sortBy = "cpu"
			m.sortServers()
			return m, nil
		case " ":
			m.sortDesc = !m.sortDesc
			m.sortServers()
			return m, nil
		case "h", "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "F5", "ctrl+r":
			// Force refresh
			return m, m.fetchServers()
		}

	case tickMsg:
		return m, tea.Batch(
			tea.Tick(m.refreshRate, func(t time.Time) tea.Msg {
				return tickMsg(t)
			}),
			m.fetchServers(),
		)

	case serversMsg:
		m.servers = []ServerInfo(msg)
		m.lastUpdate = time.Now()
		m.sortServers()
		return m, nil

	case errMsg:
		m.err = error(msg)
		return m, nil
	}

	return m, nil
}

// View renders the TUI
func (m *TopModel) View() string {
	if m.err != nil {
		errorMsg := fmt.Sprintf("Error: %v\n\nPress q to quit", m.err)
		return statusErrorStyle.Render(errorMsg)
	}

	var b strings.Builder

	// Enhanced header with borders
	headerBox := lipgloss.NewStyle().
		Foreground(ArmyGreen).
		Bold(true).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Brown).
		Padding(0, 2).
		Margin(1, 0)
	
	b.WriteString(headerBox.Render("╔═══ Matey Top - MCP Server Monitor ═══╗"))
	b.WriteString("\n")

	// Enhanced status bar with better formatting
	currentTime := m.lastUpdate.Format("15:04:05")
	serverCount := len(m.servers)
	sortDirection := "↑"
	if m.sortDesc {
		sortDirection = "↓"
	}
	
	statusBar := fmt.Sprintf("Last Update: %s │ Servers: %d │ Sort: %s %s │ Press 'h' for help",
		currentTime, serverCount, m.sortBy, sortDirection)
	
	statusBarBox := lipgloss.NewStyle().
		Foreground(GoldYellow).
		Bold(true).
		Padding(0, 2).
		Margin(0, 1)
	
	b.WriteString(statusBarBox.Render(statusBar))
	b.WriteString("\n\n")

	// Enhanced help section
	if m.showHelp {
		helpContent := `╔═══ Controls ═══╗
│ q/Ctrl+C: Quit          │ h/?: Toggle help       │ Space: Reverse sort   │
│ F5/Ctrl+R: Force refresh                                                 │
├─── Sort Options ────────────────────────────────────────────────────────┤
│ n: Sort by name         │ s: Sort by status      │ t: Sort by type       │
│ r: Sort by restarts     │ a: Sort by age         │ p: Sort by protocol   │
│ c: Sort by CPU usage    │ m: Sort by memory      │                       │
╚══════════════════════════════════════════════════════════════════════════╝`
		
		helpBox := lipgloss.NewStyle().
			Foreground(Yellow).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Brown).
			Padding(1, 2).
			Margin(1, 1)
		
		b.WriteString(helpBox.Render(helpContent))
		b.WriteString("\n")
	}

	// Enhanced table header with better spacing
	headerRow := fmt.Sprintf("%-20s %-10s %-12s %-8s %-10s %-8s %-8s %-30s %-10s %-6s",
		"NAME", "STATUS", "TYPE", "RESTARTS", "AGE", "CPU", "MEMORY", "IMAGE", "PROTOCOL", "PORT")
	
	headerFormatted := headerStyle.Render(headerRow)
	b.WriteString(headerFormatted)
	b.WriteString("\n")

	// Enhanced separator with styled line
	separatorWidth := m.width
	if separatorWidth == 0 {
		separatorWidth = 140
	}
	separator := strings.Repeat("─", separatorWidth-4)
	b.WriteString(separatorStyle.Render("├" + separator + "┤"))
	b.WriteString("\n")

	// Server rows with enhanced formatting
	if len(m.servers) == 0 {
		noServersMsg := "No servers found. Check your configuration and try again."
		noServersBox := lipgloss.NewStyle().
			Foreground(Tan).
			Italic(true).
			Padding(2, 4).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Brown)
		
		b.WriteString(noServersBox.Render(noServersMsg))
	} else {
		for i, server := range m.servers {
			row := m.formatServerRow(server, i%2 == 0) // Alternate row styling
			b.WriteString(row)
			b.WriteString("\n")
		}
	}

	// Enhanced footer
	b.WriteString("\n")
	footerContent := "Matey MCP Server Orchestrator - Real-time status monitoring"
	footerBox := lipgloss.NewStyle().
		Foreground(Brown).
		Italic(true).
		Align(lipgloss.Center)
	
	b.WriteString(footerBox.Render(footerContent))

	return b.String()
}

// formatServerRow formats a server row with appropriate styling and alternating colors
func (m *TopModel) formatServerRow(server ServerInfo, isEvenRow bool) string {
	// Format status with color
	var statusStr string
	switch server.Status {
	case "running":
		statusStr = statusRunningStyle.Render(server.Status)
	case "pending", "starting":
		statusStr = statusPendingStyle.Render(server.Status)
	case "error", "failed":
		statusStr = statusErrorStyle.Render(server.Status)
	default:
		statusStr = statusUnknownStyle.Render(server.Status)
	}

	// Format age with color based on duration
	ageStr := m.formatDurationWithColor(server.Age)

	// Format image (truncate if too long) with subtle styling
	imageStr := server.Image
	if len(imageStr) > 30 {
		imageStr = imageStr[:27] + "..."
	}
	imageStyled := lipgloss.NewStyle().Foreground(Tan).Render(imageStr)

	// Format port with styling
	portStr := ""
	if server.Port > 0 {
		portStr = lipgloss.NewStyle().Foreground(GoldYellow).Render(fmt.Sprintf("%d", server.Port))
	}

	// Format restart count with color (red if > 0)
	restartsStr := fmt.Sprintf("%d", server.Restarts)
	if server.Restarts > 0 {
		restartsStr = statusErrorStyle.Render(restartsStr)
	} else {
		restartsStr = statusRunningStyle.Render(restartsStr)
	}

	// Format resource usage with colors
	cpuStr := server.CPU
	memStr := server.Memory
	if cpuStr != "" {
		cpuStr = lipgloss.NewStyle().Foreground(Brown).Render(cpuStr)
	}
	if memStr != "" {
		memStr = lipgloss.NewStyle().Foreground(Brown).Render(memStr)
	}

	// Format server name with type-based coloring
	nameStr := server.Name
	switch server.Type {
	case "matey-core":
		nameStr = lipgloss.NewStyle().Foreground(ArmyGreen).Bold(true).Render(nameStr)
	case "mcp-server":
		nameStr = lipgloss.NewStyle().Foreground(LightGreen).Render(nameStr)
	case "memory":
		nameStr = lipgloss.NewStyle().Foreground(GoldYellow).Render(nameStr)
	case "task-scheduler":
		nameStr = lipgloss.NewStyle().Foreground(Yellow).Render(nameStr)
	default:
		nameStr = lipgloss.NewStyle().Foreground(Tan).Render(nameStr)
	}

	// Format protocol with appropriate color
	protocolStr := server.Protocol
	if protocolStr != "" {
		switch protocolStr {
		case "http", "https":
			protocolStr = lipgloss.NewStyle().Foreground(LightGreen).Render(protocolStr)
		case "sse":
			protocolStr = lipgloss.NewStyle().Foreground(GoldYellow).Render(protocolStr)
		case "websocket":
			protocolStr = lipgloss.NewStyle().Foreground(Yellow).Render(protocolStr)
		case "stdio":
			protocolStr = lipgloss.NewStyle().Foreground(Brown).Render(protocolStr)
		default:
			protocolStr = lipgloss.NewStyle().Foreground(Tan).Render(protocolStr)
		}
	}

	// Create the formatted row
	row := fmt.Sprintf("%-20s %-10s %-12s %-8s %-10s %-8s %-8s %-30s %-10s %-6s",
		nameStr,
		statusStr,
		server.Type,
		restartsStr,
		ageStr,
		cpuStr,
		memStr,
		imageStyled,
		protocolStr,
		portStr)

	// Apply subtle alternating row background
	if isEvenRow {
		// Slightly darker background for even rows
		rowStyle := lipgloss.NewStyle().Background(lipgloss.Color("235"))
		return rowStyle.Render(row)
	}
	
	return row
}

// formatDurationWithColor formats duration with color coding
func (m *TopModel) formatDurationWithColor(d time.Duration) string {
	durationStr := formatDuration(d)
	
	// Color code based on age
	if d < time.Hour {
		// New - bright green
		return lipgloss.NewStyle().Foreground(LightGreen).Bold(true).Render(durationStr)
	} else if d < 24*time.Hour {
		// Recent - yellow
		return lipgloss.NewStyle().Foreground(GoldYellow).Render(durationStr)
	} else {
		// Old - tan
		return lipgloss.NewStyle().Foreground(Tan).Render(durationStr)
	}
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// sortServers sorts the servers based on the current sort criteria
func (m *TopModel) sortServers() {
	sort.Slice(m.servers, func(i, j int) bool {
		var less bool
		switch m.sortBy {
		case "name":
			less = m.servers[i].Name < m.servers[j].Name
		case "status":
			less = m.servers[i].Status < m.servers[j].Status
		case "restarts":
			less = m.servers[i].Restarts < m.servers[j].Restarts
		case "age":
			less = m.servers[i].Age < m.servers[j].Age
		case "type":
			less = m.servers[i].Type < m.servers[j].Type
		case "protocol":
			less = m.servers[i].Protocol < m.servers[j].Protocol
		case "memory":
			// Handle memory sorting (parse memory values for numeric comparison)
			memI := parseMemoryValue(m.servers[i].Memory)
			memJ := parseMemoryValue(m.servers[j].Memory)
			less = memI < memJ
		case "cpu":
			// Handle CPU sorting (parse CPU values for numeric comparison)
			cpuI := parseCPUValue(m.servers[i].CPU)
			cpuJ := parseCPUValue(m.servers[j].CPU)
			less = cpuI < cpuJ
		default:
			less = m.servers[i].Name < m.servers[j].Name
		}
		
		if m.sortDesc {
			return !less
		}
		return less
	})
}

// parseMemoryValue converts memory strings like "128Mi", "1Gi" to bytes for comparison
func parseMemoryValue(memory string) int64 {
	if memory == "" {
		return 0
	}
	
	memory = strings.TrimSpace(memory)
	
	// Handle common Kubernetes memory formats
	if strings.HasSuffix(memory, "Mi") {
		if val, err := fmt.Sscanf(memory, "%dMi", new(int64)); err == nil && val == 1 {
			var mem int64
			fmt.Sscanf(memory, "%dMi", &mem)
			return mem * 1024 * 1024 // Convert Mi to bytes
		}
	}
	if strings.HasSuffix(memory, "Gi") {
		if val, err := fmt.Sscanf(memory, "%dGi", new(int64)); err == nil && val == 1 {
			var mem int64
			fmt.Sscanf(memory, "%dGi", &mem)
			return mem * 1024 * 1024 * 1024 // Convert Gi to bytes
		}
	}
	if strings.HasSuffix(memory, "Ki") {
		if val, err := fmt.Sscanf(memory, "%dKi", new(int64)); err == nil && val == 1 {
			var mem int64
			fmt.Sscanf(memory, "%dKi", &mem)
			return mem * 1024 // Convert Ki to bytes
		}
	}
	
	// Try to parse as plain number (assume bytes)
	if val, err := fmt.Sscanf(memory, "%d", new(int64)); err == nil && val == 1 {
		var mem int64
		fmt.Sscanf(memory, "%d", &mem)
		return mem
	}
	
	return 0
}

// parseCPUValue converts CPU strings like "100m", "1" to millicores for comparison
func parseCPUValue(cpu string) int64 {
	if cpu == "" {
		return 0
	}
	
	cpu = strings.TrimSpace(cpu)
	
	// Handle millicores
	if strings.HasSuffix(cpu, "m") {
		if val, err := fmt.Sscanf(cpu, "%dm", new(int64)); err == nil && val == 1 {
			var cpuVal int64
			fmt.Sscanf(cpu, "%dm", &cpuVal)
			return cpuVal // Already in millicores
		}
	}
	
	// Handle whole cores (convert to millicores)
	if val, err := fmt.Sscanf(cpu, "%d", new(int64)); err == nil && val == 1 {
		var cpuVal int64
		fmt.Sscanf(cpu, "%d", &cpuVal)
		return cpuVal * 1000 // Convert to millicores
	}
	
	return 0
}

// fetchServers fetches server information
func (m *TopModel) fetchServers() tea.Cmd {
	return tea.Cmd(func() tea.Msg {
		servers, err := m.getServerInfo()
		if err != nil {
			return errMsg(err)
		}
		return serversMsg(servers)
	})
}

// getServerInfo gets detailed information about all servers
func (m *TopModel) getServerInfo() ([]ServerInfo, error) {
	var servers []ServerInfo

	// Get basic status from composer
	status, err := m.composer.Status()
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}

	// Get detailed information from Kubernetes
	ctx := context.Background()
	deployments, err := m.k8sClient.AppsV1().Deployments("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	pods, err := m.k8sClient.CoreV1().Pods("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	// Create a map of deployments for quick lookup
	deploymentMap := make(map[string]*appsv1.Deployment)
	for i := range deployments.Items {
		dep := &deployments.Items[i]
		deploymentMap[dep.Name] = dep
	}

	// Create a map of pods for quick lookup
	podMap := make(map[string][]*corev1.Pod)
	for i := range pods.Items {
		pod := &pods.Items[i]
		if appLabel, ok := pod.Labels["app"]; ok {
			podMap[appLabel] = append(podMap[appLabel], pod)
		}
	}

	// Build server info
	for name, svcStatus := range status.Services {
		server := ServerInfo{
			Name:        name,
			Status:      svcStatus.Status,
			Type:        svcStatus.Type,
			LastUpdated: time.Now(),
		}

		// Get deployment info
		if dep, exists := deploymentMap[name]; exists {
			server.Age = time.Since(dep.CreationTimestamp.Time)
			server.Replicas = fmt.Sprintf("%d/%d", dep.Status.ReadyReplicas, dep.Status.Replicas)
			server.Ready = dep.Status.ReadyReplicas > 0

			// Get container info
			if len(dep.Spec.Template.Spec.Containers) > 0 {
				container := dep.Spec.Template.Spec.Containers[0]
				server.Image = container.Image
				
				// Get resource info
				if container.Resources.Requests != nil {
					if cpu, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
						server.CPU = cpu.String()
					}
					if memory, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
						server.Memory = memory.String()
					}
				}

				// Get port info
				if len(container.Ports) > 0 {
					server.Port = container.Ports[0].ContainerPort
				}
			}

			// Get protocol from labels
			if protocol, ok := dep.Labels["mcp.matey.ai/protocol"]; ok {
				server.Protocol = protocol
			}
		}

		// Get pod info for restarts
		if podList, exists := podMap[name]; exists && len(podList) > 0 {
			var totalRestarts int32
			for _, pod := range podList {
				for _, containerStatus := range pod.Status.ContainerStatuses {
					totalRestarts += containerStatus.RestartCount
				}
			}
			server.Restarts = totalRestarts
		}

		servers = append(servers, server)
	}

	return servers, nil
}