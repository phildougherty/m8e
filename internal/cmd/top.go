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

// Styles for the TUI
var (
	titleStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("205")).
		Bold(true).
		Margin(1, 0)

	headerStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Bold(true).
		Underline(true)

	statusRunningStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("46")).
		Bold(true)

	statusPendingStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("226")).
		Bold(true)

	statusErrorStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")).
		Bold(true)

	statusUnknownStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Bold(true)

	tableStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("238"))

	helpStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Margin(1, 0)
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
		Long: `Display a live view of MCP servers with detailed information including:
- Real-time status updates
- Resource usage (CPU, Memory)
- Restart counts and age
- Container information
- Network configuration
- Interactive sorting and filtering

Controls:
- q/Ctrl+C: Quit
- s: Sort by status
- n: Sort by name
- r: Sort by restarts
- a: Sort by age
- h: Toggle help
- Space: Reverse sort order`,
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
		case " ":
			m.sortDesc = !m.sortDesc
			m.sortServers()
			return m, nil
		case "h":
			m.showHelp = !m.showHelp
			return m, nil
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
		return fmt.Sprintf("Error: %v\n\nPress q to quit", m.err)
	}

	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render("Matey Top - MCP Server Monitor"))
	b.WriteString("\n")

	// Status bar
	statusBar := fmt.Sprintf("Last Update: %s | Servers: %d | Sort: %s %s | Press 'h' for help",
		m.lastUpdate.Format("15:04:05"),
		len(m.servers),
		m.sortBy,
		func() string {
			if m.sortDesc {
				return "↓"
			}
			return "↑"
		}())
	b.WriteString(helpStyle.Render(statusBar))
	b.WriteString("\n")

	// Help section
	if m.showHelp {
		help := `Controls:
  q/Ctrl+C: Quit          s: Sort by status      n: Sort by name
  r: Sort by restarts     a: Sort by age         Space: Reverse sort
  h: Toggle this help
`
		b.WriteString(tableStyle.Render(help))
		b.WriteString("\n")
	}

	// Table header
	header := fmt.Sprintf("%-20s %-10s %-12s %-8s %-10s %-8s %-8s %-30s %-10s %-6s",
		"NAME", "STATUS", "TYPE", "RESTARTS", "AGE", "CPU", "MEMORY", "IMAGE", "PROTOCOL", "PORT")
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	// Separator
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")

	// Server rows
	for _, server := range m.servers {
		row := m.formatServerRow(server)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Footer
	if len(m.servers) == 0 {
		b.WriteString(helpStyle.Render("No servers found"))
	}

	return b.String()
}

// formatServerRow formats a server row with appropriate styling
func (m *TopModel) formatServerRow(server ServerInfo) string {
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

	// Format age
	ageStr := formatDuration(server.Age)

	// Format image (truncate if too long)
	imageStr := server.Image
	if len(imageStr) > 30 {
		imageStr = imageStr[:27] + "..."
	}

	// Format port
	portStr := ""
	if server.Port > 0 {
		portStr = fmt.Sprintf("%d", server.Port)
	}

	return fmt.Sprintf("%-20s %-10s %-12s %-8d %-10s %-8s %-8s %-30s %-10s %-6s",
		server.Name,
		statusStr,
		server.Type,
		server.Restarts,
		ageStr,
		server.CPU,
		server.Memory,
		imageStr,
		server.Protocol,
		portStr)
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
		default:
			less = m.servers[i].Name < m.servers[j].Name
		}
		
		if m.sortDesc {
			return !less
		}
		return less
	})
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