{{/*
Expand the name of the chart.
*/}}
{{- define "milvus-coredump-agent.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "milvus-coredump-agent.fullname" -}}
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
{{- define "milvus-coredump-agent.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "milvus-coredump-agent.labels" -}}
helm.sh/chart: {{ include "milvus-coredump-agent.chart" . }}
{{ include "milvus-coredump-agent.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "milvus-coredump-agent.selectorLabels" -}}
app.kubernetes.io/name: {{ include "milvus-coredump-agent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "milvus-coredump-agent.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "milvus-coredump-agent.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Get the controller image name
*/}}
{{- define "milvus-coredump-agent.controller.image" -}}
{{- $registry := .Values.controller.image.repository | default .Values.global.image.registry -}}
{{- $repository := .Values.controller.image.repository | default .Values.global.image.repository -}}
{{- $tag := .Values.controller.image.tag | default .Values.global.image.tag -}}
{{- if $registry -}}
{{- printf "%s/%s:%s" $registry $repository $tag -}}
{{- else -}}
{{- printf "%s:%s" $repository $tag -}}
{{- end -}}
{{- end -}}

{{/*
Get the agent image name
*/}}
{{- define "milvus-coredump-agent.agent.image" -}}
{{- $registry := .Values.agent.image.repository | default .Values.global.image.registry -}}
{{- $repository := .Values.agent.image.repository | default .Values.global.image.repository -}}
{{- $tag := .Values.agent.image.tag | default .Values.global.image.tag -}}
{{- if $registry -}}
{{- printf "%s/%s:%s" $registry $repository $tag -}}
{{- else -}}
{{- printf "%s:%s" $repository $tag -}}
{{- end -}}
{{- end -}}

{{/*
Get the controller pull policy
*/}}
{{- define "milvus-coredump-agent.controller.pullPolicy" -}}
{{- .Values.controller.image.pullPolicy | default .Values.global.image.pullPolicy -}}
{{- end -}}

{{/*
Get the agent pull policy
*/}}
{{- define "milvus-coredump-agent.agent.pullPolicy" -}}
{{- .Values.agent.image.pullPolicy | default .Values.global.image.pullPolicy -}}
{{- end -}}