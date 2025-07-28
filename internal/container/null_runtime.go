package container

import (
	"fmt"
	"io"
	"os/exec"
)

// NullRuntime implements the Runtime interface for testing purposes
// All methods return appropriate no-op responses or test-friendly errors
type NullRuntime struct {
	containers map[string]*ContainerInfo
	images     map[string]*ImageInfo
	volumes    map[string]*VolumeInfo
	networks   map[string]*NetworkInfo
}

// NewNullRuntime creates a new null runtime for testing
func NewNullRuntime() *NullRuntime {
	return &NullRuntime{
		containers: make(map[string]*ContainerInfo),
		images:     make(map[string]*ImageInfo),
		volumes:    make(map[string]*VolumeInfo),
		networks:   make(map[string]*NetworkInfo),
	}
}

// Container lifecycle management
func (n *NullRuntime) StartContainer(opts *ContainerOptions) (string, error) {
	if opts == nil {
		return "", fmt.Errorf("container options cannot be nil")
	}
	
	containerID := fmt.Sprintf("null-container-%s", opts.Name)
	n.containers[opts.Name] = &ContainerInfo{
		ID:     containerID,
		Name:   opts.Name,
		Image:  opts.Image,
		Status: "running",
		State:  "running",
	}
	
	return containerID, nil
}

func (n *NullRuntime) StopContainer(name string) error {
	if info, exists := n.containers[name]; exists {
		info.Status = "stopped"
		info.State = "exited"
		return nil
	}
	return fmt.Errorf("container %s not found", name)
}

func (n *NullRuntime) RestartContainer(name string) error {
	if info, exists := n.containers[name]; exists {
		info.Status = "running"
		info.State = "running"
		return nil
	}
	return fmt.Errorf("container %s not found", name)
}

func (n *NullRuntime) PauseContainer(name string) error {
	if info, exists := n.containers[name]; exists {
		info.Status = "paused"
		info.State = "paused"
		return nil
	}
	return fmt.Errorf("container %s not found", name)
}

func (n *NullRuntime) UnpauseContainer(name string) error {
	if info, exists := n.containers[name]; exists {
		info.Status = "running"
		info.State = "running"
		return nil
	}
	return fmt.Errorf("container %s not found", name)
}

// Container inspection and monitoring
func (n *NullRuntime) GetContainerStatus(name string) (string, error) {
	if info, exists := n.containers[name]; exists {
		return info.Status, nil
	}
	return "unknown", fmt.Errorf("container %s not found", name)
}

func (n *NullRuntime) GetContainerInfo(name string) (*ContainerInfo, error) {
	if info, exists := n.containers[name]; exists {
		return info, nil
	}
	return nil, fmt.Errorf("container %s not found", name)
}

func (n *NullRuntime) ListContainers(filters map[string]string) ([]ContainerInfo, error) {
	var containers []ContainerInfo
	for _, info := range n.containers {
		containers = append(containers, *info)
	}
	return containers, nil
}

func (n *NullRuntime) GetContainerStats(name string) (*ContainerStats, error) {
	if _, exists := n.containers[name]; exists {
		return &ContainerStats{
			CPUUsage:    0.0,
			MemoryUsage: 0,
			MemoryLimit: 0,
		}, nil
	}
	return nil, fmt.Errorf("container %s not found", name)
}

func (n *NullRuntime) WaitForContainer(name string, condition string) error {
	if _, exists := n.containers[name]; exists {
		return nil
	}
	return fmt.Errorf("container %s not found", name)
}

// Container logs and execution
func (n *NullRuntime) ShowContainerLogs(name string, follow bool) error {
	if _, exists := n.containers[name]; exists {
		return nil
	}
	return fmt.Errorf("container %s not found", name)
}

func (n *NullRuntime) ExecContainer(containerName string, command []string, interactive bool) (*exec.Cmd, io.Writer, io.Reader, error) {
	if _, exists := n.containers[containerName]; exists {
		return nil, nil, nil, fmt.Errorf("exec not supported in null runtime")
	}
	return nil, nil, nil, fmt.Errorf("container %s not found", containerName)
}

// Image management
func (n *NullRuntime) PullImage(image string, auth *ImageAuth) error {
	n.images[image] = &ImageInfo{
		ID:   fmt.Sprintf("null-image-%s", image),
		Tags: []string{image},
		Size: 0,
	}
	return nil
}

func (n *NullRuntime) BuildImage(opts *BuildOptions) error {
	if opts == nil {
		return fmt.Errorf("build options cannot be nil")
	}
	
	for _, tag := range opts.Tags {
		n.images[tag] = &ImageInfo{
			ID:   fmt.Sprintf("null-image-%s", tag),
			Tags: []string{tag},
			Size: 0,
		}
	}
	return nil
}

func (n *NullRuntime) RemoveImage(image string, force bool) error {
	if _, exists := n.images[image]; exists {
		delete(n.images, image)
		return nil
	}
	return fmt.Errorf("image %s not found", image)
}

func (n *NullRuntime) ListImages() ([]ImageInfo, error) {
	var images []ImageInfo
	for _, info := range n.images {
		images = append(images, *info)
	}
	return images, nil
}

// Volume management
func (n *NullRuntime) CreateVolume(name string, opts *VolumeOptions) error {
	n.volumes[name] = &VolumeInfo{
		Name:   name,
		Driver: "null",
		Scope:  "local",
	}
	return nil
}

func (n *NullRuntime) RemoveVolume(name string, force bool) error {
	if _, exists := n.volumes[name]; exists {
		delete(n.volumes, name)
		return nil
	}
	return fmt.Errorf("volume %s not found", name)
}

func (n *NullRuntime) ListVolumes() ([]VolumeInfo, error) {
	var volumes []VolumeInfo
	for _, info := range n.volumes {
		volumes = append(volumes, *info)
	}
	return volumes, nil
}

// Network management
func (n *NullRuntime) NetworkExists(name string) (bool, error) {
	_, exists := n.networks[name]
	return exists, nil
}

func (n *NullRuntime) CreateNetwork(name string) error {
	n.networks[name] = &NetworkInfo{
		ID:     fmt.Sprintf("null-network-%s", name),
		Name:   name,
		Driver: "null",
		Scope:  "local",
	}
	return nil
}

func (n *NullRuntime) RemoveNetwork(name string) error {
	if _, exists := n.networks[name]; exists {
		delete(n.networks, name)
		return nil
	}
	return fmt.Errorf("network %s not found", name)
}

func (n *NullRuntime) ListNetworks() ([]NetworkInfo, error) {
	var networks []NetworkInfo
	for _, info := range n.networks {
		networks = append(networks, *info)
	}
	return networks, nil
}

func (n *NullRuntime) GetNetworkInfo(name string) (*NetworkInfo, error) {
	if info, exists := n.networks[name]; exists {
		return info, nil
	}
	return nil, fmt.Errorf("network %s not found", name)
}

func (n *NullRuntime) ConnectToNetwork(containerName, networkName string) error {
	if _, exists := n.containers[containerName]; !exists {
		return fmt.Errorf("container %s not found", containerName)
	}
	if _, exists := n.networks[networkName]; !exists {
		return fmt.Errorf("network %s not found", networkName)
	}
	return nil
}

func (n *NullRuntime) DisconnectFromNetwork(containerName, networkName string) error {
	if _, exists := n.containers[containerName]; !exists {
		return fmt.Errorf("container %s not found", containerName)
	}
	if _, exists := n.networks[networkName]; !exists {
		return fmt.Errorf("network %s not found", networkName)
	}
	return nil
}

// Resource management
func (n *NullRuntime) UpdateContainerResources(name string, resources *ResourceLimits) error {
	if _, exists := n.containers[name]; exists {
		return nil
	}
	return fmt.Errorf("container %s not found", name)
}

// Security and validation
func (n *NullRuntime) ValidateSecurityContext(opts *ContainerOptions) error {
	if opts == nil {
		return fmt.Errorf("container options cannot be nil")
	}
	return nil
}

// Runtime information
func (n *NullRuntime) GetRuntimeName() string {
	return "null"
}