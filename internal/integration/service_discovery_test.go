package integration

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ctrl "sigs.k8s.io/controller-runtime"

	mcpv1 "github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/discovery"
)

// TestServiceDiscoveryIntegration tests the complete service discovery flow
func TestServiceDiscoveryIntegration(t *testing.T) {
	// Create fake Kubernetes client
	fakeClient := fake.NewSimpleClientset()
	
	// Create service discovery manager with fake client integration
	serviceDiscovery := &discovery.K8sServiceDiscovery{
		Client:    fakeClient,
		Namespace: "matey",
		Logger:    nil,
	}

	// Create connection manager
	connectionManager := &discovery.DynamicConnectionManager{}

	t.Run("discover_and_connect_to_mcp_services", func(t *testing.T) {
		// Phase 1: Create mock MCP services
		httpService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "http-mcp-server",
				Namespace: "matey",
				Labels: map[string]string{
					"mcp.matey.ai/role":         "server",
					"mcp.matey.ai/protocol":     "http",
					"mcp.matey.ai/server-name":  "http-mcp-server",
					"mcp.matey.ai/capabilities": "tools.resources",
				},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name:     "http",
						Port:     8080,
						Protocol: corev1.ProtocolTCP,
					},
				},
				Selector: map[string]string{
					"app": "http-mcp-server",
				},
			},
		}

		sseService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sse-mcp-server",
				Namespace: "matey",
				Labels: map[string]string{
					"mcp.matey.ai/role":         "server",
					"mcp.matey.ai/protocol":     "sse",
					"mcp.matey.ai/server-name":  "sse-mcp-server",
					"mcp.matey.ai/capabilities": "tools",
				},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name:     "sse",
						Port:     8080,
						Protocol: corev1.ProtocolTCP,
					},
				},
				Selector: map[string]string{
					"app": "sse-mcp-server",
				},
			},
		}

		// Create services in fake client
		_, err := fakeClient.CoreV1().Services("default").Create(context.TODO(), httpService, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create HTTP service: %v", err)
		}

		_, err = fakeClient.CoreV1().Services("default").Create(context.TODO(), sseService, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create SSE service: %v", err)
		}

		// Phase 2: Discover services
		ctx := context.Background()
		services, err := serviceDiscovery.DiscoverMCPServices(ctx)
		if err != nil {
			t.Fatalf("Failed to discover services: %v", err)
		}

		// Verify discovered services
		if len(services) != 2 {
			t.Errorf("Expected 2 services, got %d", len(services))
		}

		// Check HTTP service
		httpFound := false
		sseFound := false
		for _, service := range services {
			switch service.Name {
			case "http-mcp-server":
				httpFound = true
				if service.Protocol != "http" {
					t.Errorf("Expected HTTP protocol, got %s", service.Protocol)
				}
				if service.Port != 8080 {
					t.Errorf("Expected port 8080, got %d", service.Port)
				}
				if service.Endpoint != "http-mcp-server.matey.svc.cluster.local:8080" {
					t.Errorf("Expected endpoint http-mcp-server.matey.svc.cluster.local:8080, got %s", service.Endpoint)
				}
				expectedCaps := []string{"tools", "resources"}
				if len(service.Capabilities) != len(expectedCaps) {
					t.Errorf("Expected %d capabilities, got %d", len(expectedCaps), len(service.Capabilities))
				}

			case "sse-mcp-server":
				sseFound = true
				if service.Protocol != "sse" {
					t.Errorf("Expected SSE protocol, got %s", service.Protocol)
				}
				if service.Port != 8080 {
					t.Errorf("Expected port 8080, got %d", service.Port)
				}
				expectedCaps := []string{"tools"}
				if len(service.Capabilities) != len(expectedCaps) {
					t.Errorf("Expected %d capabilities, got %d", len(expectedCaps), len(service.Capabilities))
				}
			}
		}

		if !httpFound {
			t.Error("HTTP service not found in discovery results")
		}
		if !sseFound {
			t.Error("SSE service not found in discovery results")
		}

		// Phase 3: Test connection management
		// This would typically involve creating actual connections
		// For this test, we'll simulate connection creation
		for _, service := range services {
			connection := &discovery.MCPConnection{
				Name:         service.Name,
				Endpoint:     service.Endpoint,
				Protocol:     service.Protocol,
				Port:         service.Port,
				Capabilities: service.Capabilities,
				Status:       "connected",
				LastSeen:     time.Now(),
			}

			err := connectionManager.AddConnection(connection)
			if err != nil {
				t.Errorf("Failed to add connection for %s: %v", service.Name, err)
			}
		}

		// Verify connections were added
		connections := connectionManager.GetConnections()
		if len(connections) != 2 {
			t.Errorf("Expected 2 connections, got %d", len(connections))
		}

		// Test getting specific connection
		httpConn, err := connectionManager.GetConnection("http-mcp-server")
		if err != nil {
			t.Fatalf("Failed to get connection: %v", err)
		}
		if httpConn == nil {
			t.Error("Expected to find HTTP connection")
		} else if httpConn.Protocol != "http" {
			t.Errorf("Expected HTTP protocol, got %s", httpConn.Protocol)
		}

		// Phase 4: Test service filtering
		httpServices, err := serviceDiscovery.DiscoverMCPServicesByProtocol(ctx, "http")
		if err != nil {
			t.Fatalf("Failed to discover HTTP services: %v", err)
		}

		if len(httpServices) != 1 {
			t.Errorf("Expected 1 HTTP service, got %d", len(httpServices))
		}

		if httpServices[0].Name != "http-mcp-server" {
			t.Errorf("Expected http-mcp-server, got %s", httpServices[0].Name)
		}

		// Test capability filtering
		toolServices, err := serviceDiscovery.DiscoverMCPServicesByCapability(ctx, "tools")
		if err != nil {
			t.Fatalf("Failed to discover services with tools capability: %v", err)
		}

		if len(toolServices) != 2 {
			t.Errorf("Expected 2 services with tools capability, got %d", len(toolServices))
		}

		resourceServices, err := serviceDiscovery.DiscoverMCPServicesByCapability(ctx, "resources")
		if err != nil {
			t.Fatalf("Failed to discover services with resources capability: %v", err)
		}

		if len(resourceServices) != 1 {
			t.Errorf("Expected 1 service with resources capability, got %d", len(resourceServices))
		}

		// Phase 5: Test connection cleanup
		err = connectionManager.RemoveConnection("http-mcp-server")
		if err != nil {
			t.Errorf("Failed to remove HTTP connection: %v", err)
		}

		connections = connectionManager.GetConnections()
		if len(connections) != 1 {
			t.Errorf("Expected 1 connection after removal, got %d", len(connections))
		}

		// Verify the remaining connection is the SSE one
		if connections[0].Name != "sse-mcp-server" {
			t.Errorf("Expected sse-mcp-server to remain, got %s", connections[0].Name)
		}
	})
}

// TestServiceDiscoveryWatcher tests the service watcher functionality
func TestServiceDiscoveryWatcher(t *testing.T) {
	// Create fake Kubernetes client
	fakeClient := fake.NewSimpleClientset()
	
	// Create service discovery manager
	serviceDiscovery := &discovery.K8sServiceDiscovery{
		Client:    fakeClient,
		Namespace: "matey",
		Logger:    nil,
	}

	t.Run("watch_service_changes", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Start watching (this would normally be a long-running process)
		// For this test, we'll simulate the watch behavior
		
		// Create initial service
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "watched-mcp-server",
				Namespace: "matey",
				Labels: map[string]string{
					"mcp.matey.ai/role":        "server",
					"mcp.matey.ai/protocol":    "http",
					"mcp.matey.ai/server-name": "watched-mcp-server",
				},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name:     "http",
						Port:     8080,
						Protocol: corev1.ProtocolTCP,
					},
				},
				Selector: map[string]string{
					"app": "watched-mcp-server",
				},
			},
		}

		_, err := fakeClient.CoreV1().Services("default").Create(ctx, service, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create watched service: %v", err)
		}

		// Verify service can be discovered
		services, err := serviceDiscovery.DiscoverMCPServices(ctx)
		if err != nil {
			t.Fatalf("Failed to discover services: %v", err)
		}

		if len(services) != 1 {
			t.Errorf("Expected 1 service, got %d", len(services))
		}

		// Simulate service update (add capabilities)
		service.Labels["mcp.matey.ai/capabilities"] = "tools.resources"
		_, err = fakeClient.CoreV1().Services("default").Update(ctx, service, metav1.UpdateOptions{})
		if err != nil {
			t.Fatalf("Failed to update service: %v", err)
		}

		// Verify updated service
		services, err = serviceDiscovery.DiscoverMCPServices(ctx)
		if err != nil {
			t.Fatalf("Failed to discover updated services: %v", err)
		}

		if len(services) != 1 {
			t.Errorf("Expected 1 service after update, got %d", len(services))
		}

		updatedService := services[0]
		if len(updatedService.Capabilities) != 2 {
			t.Errorf("Expected 2 capabilities after update, got %d", len(updatedService.Capabilities))
		}

		// Simulate service deletion
		err = fakeClient.CoreV1().Services("default").Delete(ctx, service.Name, metav1.DeleteOptions{})
		if err != nil {
			t.Fatalf("Failed to delete service: %v", err)
		}

		// Verify service is no longer discovered
		services, err = serviceDiscovery.DiscoverMCPServices(ctx)
		if err != nil {
			t.Fatalf("Failed to discover services after deletion: %v", err)
		}

		if len(services) != 0 {
			t.Errorf("Expected 0 services after deletion, got %d", len(services))
		}
	})
}

// TestConnectionManagerHealthCheck tests connection health checking
func TestConnectionManagerHealthCheck(t *testing.T) {
	connectionManager := &discovery.DynamicConnectionManager{}

	t.Run("connection_health_monitoring", func(t *testing.T) {
		// Add a healthy connection
		healthyConn := &discovery.MCPConnection{
			Name:         "healthy-server",
			Endpoint:     "healthy-server.matey.svc.cluster.local:8080",
			Protocol:     "http",
			Port:         8080,
			Capabilities: []string{"tools"},
			Status:       "connected",
			LastSeen:     time.Now(),
		}

		err := connectionManager.AddConnection(healthyConn)
		if err != nil {
			t.Fatalf("Failed to add healthy connection: %v", err)
		}

		// Add an unhealthy connection (old LastSeen)
		unhealthyConn := &discovery.MCPConnection{
			Name:         "unhealthy-server",
			Endpoint:     "unhealthy-server.matey.svc.cluster.local:8080",
			Protocol:     "http",
			Port:         8080,
			Capabilities: []string{"tools"},
			Status:       "disconnected",
			LastSeen:     time.Now().Add(-10 * time.Minute), // 10 minutes ago
		}

		err = connectionManager.AddConnection(unhealthyConn)
		if err != nil {
			t.Fatalf("Failed to add unhealthy connection: %v", err)
		}

		// Verify both connections exist
		connections := connectionManager.GetConnections()
		if len(connections) != 2 {
			t.Errorf("Expected 2 connections, got %d", len(connections))
		}

		// Test health checking logic
		// In a real implementation, this would involve actual health checks
		// For this test, we'll simulate the health check logic
		
		healthyConnections := connectionManager.GetHealthyConnections(5 * time.Minute)
		if len(healthyConnections) != 1 {
			t.Errorf("Expected 1 healthy connection, got %d", len(healthyConnections))
		}

		if healthyConnections[0].Name != "healthy-server" {
			t.Errorf("Expected healthy-server to be healthy, got %s", healthyConnections[0].Name)
		}

		// Get all connections to check the status of the disconnected one
		allConnections := connectionManager.GetConnections()
		var updatedConn *discovery.MCPConnection
		for _, conn := range allConnections {
			if conn.Name == "unhealthy-server" {
				updatedConn = conn
				break
			}
		}
		if updatedConn == nil {
			t.Error("Expected to find updated connection")
		} else if updatedConn.Status != "disconnected" {
			t.Errorf("Expected status disconnected, got %s", updatedConn.Status)
		}

		// Test connection cleanup
		err = connectionManager.CleanupStaleConnections(5 * time.Minute)
		if err != nil {
			t.Fatalf("Failed to cleanup stale connections: %v", err)
		}

		// After cleanup, only healthy connection should remain
		connections = connectionManager.GetConnections()
		if len(connections) != 1 {
			t.Errorf("Expected 1 connection after cleanup, got %d", len(connections))
		}

		if len(connections) > 0 && connections[0].Name != "healthy-server" {
			t.Errorf("Expected healthy-server to remain, got %s", connections[0].Name)
		}
	})
}

// TestEndToEndServiceDiscovery tests the complete end-to-end service discovery flow
func TestEndToEndServiceDiscovery(t *testing.T) {
	env := SetupTestEnvironment(t)
	defer env.TeardownTestEnvironment(t)

	t.Run("complete_service_discovery_flow", func(t *testing.T) {
		// Phase 1: Create MCPServer that will be discovered
		mcpServer := &mcpv1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "discoverable-server",
				Namespace: "matey",
			},
			Spec: mcpv1.MCPServerSpec{
				Image:        "discoverable-server:latest",
				Protocol:     "http",
				HttpPort:     8080,
				Capabilities: []string{"tools", "resources", "prompts"},
				ServiceDiscovery: mcpv1.ServiceDiscoveryConfig{
					DiscoveryLabels: map[string]string{
						"team":        "platform",
						"environment": "test",
					},
				},
			},
		}

		// Create the MCPServer
		err := env.Client.Create(env.Ctx, mcpServer)
		if err != nil {
			t.Fatalf("Failed to create MCPServer: %v", err)
		}

		// Phase 2: Reconcile to create service
		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      mcpServer.Name,
				Namespace: mcpServer.Namespace,
			},
		}

		// First reconciliation - add finalizer
		_, err = env.Reconciler.Reconcile(env.Ctx, req)
		if err != nil {
			t.Fatalf("First reconciliation failed: %v", err)
		}

		// Second reconciliation - create resources
		_, err = env.Reconciler.Reconcile(env.Ctx, req)
		if err != nil {
			t.Fatalf("Second reconciliation failed: %v", err)
		}

		// Phase 3: Verify service was created with correct labels
		service := &corev1.Service{}
		err = env.Client.Get(env.Ctx, req.NamespacedName, service)
		if err != nil {
			t.Fatalf("Failed to get service: %v", err)
		}

		// Verify service discovery labels
		expectedLabels := map[string]string{
			"mcp.matey.ai/role":         "server",
			"mcp.matey.ai/protocol":     "http",
			"mcp.matey.ai/server-name":  "discoverable-server",
			"mcp.matey.ai/capabilities": "tools.resources.prompts",
			"team":                      "platform",
			"environment":               "test",
		}

		for key, expectedValue := range expectedLabels {
			if actualValue, exists := service.Labels[key]; !exists {
				t.Errorf("Expected label %s to be set", key)
			} else if actualValue != expectedValue {
				t.Errorf("Expected label %s=%s, got %s", key, expectedValue, actualValue)
			}
		}

		// Phase 4: Test service discovery would find this service
		// In a real test with actual service discovery, this would verify
		// that the service can be discovered by other components

		// Verify service has correct selector for pod discovery
		if service.Spec.Selector["app"] != mcpServer.Name {
			t.Errorf("Expected selector app=%s, got %s", mcpServer.Name, service.Spec.Selector["app"])
		}

		// Verify service has correct ports
		if len(service.Spec.Ports) == 0 {
			t.Error("Expected service to have ports")
		} else {
			port := service.Spec.Ports[0]
			if port.Port != mcpServer.Spec.HttpPort {
				t.Errorf("Expected port %d, got %d", mcpServer.Spec.HttpPort, port.Port)
			}
			if port.Name != "http" {
				t.Errorf("Expected port name http, got %s", port.Name)
			}
		}

		// Phase 5: Test that service can be queried by various filters
		// This simulates what service discovery would do

		// Test protocol-based discovery
		if service.Labels["mcp.matey.ai/protocol"] != "http" {
			t.Errorf("Expected protocol label http, got %s", service.Labels["mcp.matey.ai/protocol"])
		}

		// Test capability-based discovery
		capabilities := service.Labels["mcp.matey.ai/capabilities"]
		if capabilities != "tools.resources.prompts" {
			t.Errorf("Expected capabilities tools.resources.prompts, got %s", capabilities)
		}

		// Test custom label discovery
		if service.Labels["team"] != "platform" {
			t.Errorf("Expected team label platform, got %s", service.Labels["team"])
		}

		if service.Labels["environment"] != "test" {
			t.Errorf("Expected environment label test, got %s", service.Labels["environment"])
		}
	})
}