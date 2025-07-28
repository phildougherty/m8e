{{/*
Expand the name of the chart.
*/}}
{{- define "matey.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "matey.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "matey.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "matey.labels" -}}
helm.sh/chart: {{ include "matey.chart" . }}
{{ include "matey.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "matey.selectorLabels" -}}
app.kubernetes.io/name: {{ include "matey.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "matey.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "matey.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Registry endpoint helper
*/}}
{{- define "matey.registryEndpoint" -}}
{{- if .Values.registry.enabled }}
{{- if .Values.registry.builtin.enabled }}
{{- printf "%s-registry:%d" (include "matey.fullname" .) (.Values.registry.builtin.service.port | int) }}
{{- else }}
{{- .Values.registry.endpoint }}
{{- end }}
{{- else }}
{{- .Values.registry.endpoint | default "docker.io" }}
{{- end }}
{{- end }}

{{/*
Common environment variables for MCP servers
*/}}
{{- define "matey.commonEnv" -}}
- name: MATEY_NAMESPACE
  valueFrom:
    fieldRef:
      fieldPath: metadata.namespace
- name: MATEY_POD_NAME
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: MATEY_REGISTRY_ENDPOINT
  value: {{ include "matey.registryEndpoint" . }}
- name: MATEY_PROXY_ENDPOINT
  value: "{{ include "matey.fullname" . }}-proxy:{{ .Values.proxy.service.port }}"
{{- end }}