# Matey Project Overview

## What is Matey?

Matey, also known as m8e, is a Kubernetes-native tool designed to orchestrate Model Context Protocol, or MCP, server applications. Think of it as a modern replacement for Docker Compose, but specifically built for running multiple MCP servers together in a Kubernetes cluster.

## Core Purpose

The main goal of Matey is to make it easy to define, deploy, and manage multiple MCP servers that need to work together. Instead of manually managing individual containers or services, Matey provides a unified way to orchestrate entire MCP application stacks using Kubernetes best practices.

## Key Features

### Kubernetes-Native Architecture
Matey is built from the ground up for Kubernetes. It uses Custom Resource Definitions, or CRDs, to define MCP services and relies on Kubernetes controllers to manage their lifecycle. This means it integrates seamlessly with existing Kubernetes tooling and follows cloud-native patterns.

### Multiple Communication Protocols
Matey supports several ways for MCP servers to communicate:
- Standard HTTP for basic request-response patterns
- Server-Sent Events, or SSE, for real-time streaming
- WebSocket for bidirectional real-time communication
- Standard I/O for process-based communication, though this is limited in Kubernetes environments

### Automatic Service Discovery
One of Matey's strengths is its automatic service discovery. When you deploy MCP services, Matey automatically discovers them within the Kubernetes cluster and establishes the necessary connections. This removes the manual work of configuring service endpoints and managing connection lifecycle.

### Custom Resource Types
Matey defines four main types of custom resources:
1. MCP Server - for defining individual MCP server instances
2. MCP Memory - for managing memory services that servers can use
3. MCP Task Scheduler - for handling scheduled tasks and workflows
4. MCP Proxy - for managing proxy configurations and routing

## How It Works

### Configuration
You define your MCP application stack using YAML configuration files, similar to Docker Compose but with Kubernetes-specific options. The default configuration file is called matey.yaml.

### Deployment
When you run matey up, the tool reads your configuration and creates the appropriate Kubernetes resources. This includes deployments, services, and custom resources that define your MCP servers.

### Management
Matey provides a command-line interface for managing your MCP applications. You can start services with matey up, stop them with matey down, check their status with matey ps, and view logs with matey logs.

### Proxy and Discovery
The built-in proxy server handles routing between MCP servers and external clients. The service discovery system automatically detects when new services come online or go offline, updating connections accordingly.

## Project Structure

The codebase is organized into several key areas:

### Command Line Interface
The main entry point is in cmd/matey, with the command system built using the Cobra library in internal/cmd.

### Core Services
The internal directory contains the main application logic, including protocol implementation, Kubernetes integration, service discovery, and configuration management.

### Kubernetes Integration
Custom Resource Definitions are in internal/crd, with corresponding controllers in internal/controllers that manage the lifecycle of MCP resources.

### Deployment
The project includes Helm charts for easy Kubernetes deployment and raw Kubernetes manifests for manual deployment scenarios.

## Current State and Migration

Matey is designed to be a bridge between Docker-based development and Kubernetes-native production deployment. The configuration layer accepts Docker Compose-style syntax for familiarity, but the execution layer is fully Kubernetes-native.

This hybrid approach allows teams to:
- Use familiar Docker Compose syntax during development
- Seamlessly deploy to Kubernetes without rewriting configurations
- Take advantage of Kubernetes scalability and reliability features

## Development and Quality

The project follows Go best practices with comprehensive testing, including race condition detection and security scanning. It uses golangci-lint for static analysis and gosec for security scanning. The build system is based on Make with targets for building, testing, and quality checks.

## Authentication and Security

Matey includes built-in authentication support with OAuth integration and JWT token management. It also provides resource-based authorization and audit logging for compliance and security monitoring.

## Version Information

The current version is 0.0.4, built with Go 1.24.0 and Kubernetes client version 0.33.2. It uses the controller-runtime framework for Kubernetes integration, following standard Kubernetes controller patterns.

This overview provides a high-level understanding of what Matey does and how it fits into the cloud-native ecosystem. For detailed implementation information, refer to the project documentation and source code.