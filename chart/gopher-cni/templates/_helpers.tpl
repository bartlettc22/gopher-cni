{{/*
Expand the name of the chart.
*/}}
{{- define "gopher-cni.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "gopher-cni.fullname" -}}
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
{{- define "gopher-cni.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "gopher-cni.labels" -}}
helm.sh/chart: {{ include "gopher-cni.chart" . }}
{{ include "gopher-cni.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels (base — do not use directly on resources that need component scoping)
*/}}
{{- define "gopher-cni.selectorLabels" -}}
app.kubernetes.io/name: {{ include "gopher-cni.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Installer DaemonSet selector labels
*/}}
{{- define "gopher-cni.installerSelectorLabels" -}}
{{ include "gopher-cni.selectorLabels" . }}
app.kubernetes.io/component: installer
{{- end }}

{{/*
Webhook Deployment selector labels
*/}}
{{- define "gopher-cni.webhookSelectorLabels" -}}
{{ include "gopher-cni.selectorLabels" . }}
app.kubernetes.io/component: webhook
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "gopher-cni.serviceAccountName" -}}
{{- default (include "gopher-cni.fullname" .) .Values.serviceAccount.name }}
{{- end }}

{{/*
Create the name of the webhook service
*/}}
{{- define "gopher-cni.webhookServiceName" -}}
{{- printf "%s-webhook" (include "gopher-cni.fullname" .) }}
{{- end }}

{{/*
Create the name of the certificate
*/}}
{{- define "gopher-cni.certificateName" -}}
{{- printf "%s-webhook-cert" (include "gopher-cni.fullname" .) }}
{{- end }}

{{/*
Create the name of the certificate secret
*/}}
{{- define "gopher-cni.certificateSecretName" -}}
{{- printf "%s-webhook-certs" (include "gopher-cni.fullname" .) }}
{{- end }}

{{/*
Create the name of the CA issuer
*/}}
{{- define "gopher-cni.caIssuerName" -}}
{{- printf "%s-ca-issuer" (include "gopher-cni.fullname" .) }}
{{- end }}

{{/*
Create the name of the self-signed issuer
*/}}
{{- define "gopher-cni.selfsignedIssuerName" -}}
{{- printf "%s-selfsigned-issuer" (include "gopher-cni.fullname" .) }}
{{- end }}

{{/*
Create the name of the CA certificate
*/}}
{{- define "gopher-cni.caCertificateName" -}}
{{- printf "%s-ca" (include "gopher-cni.fullname" .) }}
{{- end }}

{{/*
Create the name of the CA secret
*/}}
{{- define "gopher-cni.caSecretName" -}}
{{- printf "%s-ca-secret" (include "gopher-cni.fullname" .) }}
{{- end }}

{{/*
Get the issuer name to use
*/}}
{{- define "gopher-cni.issuerName" -}}
{{- if .Values.webhook.certificate.issuer.create }}
{{- include "gopher-cni.caIssuerName" . }}
{{- else }}
{{- .Values.webhook.certificate.issuer.name }}
{{- end }}
{{- end }}

{{/*
Get the issuer kind
*/}}
{{- define "gopher-cni.issuerKind" -}}
{{- .Values.webhook.certificate.issuer.kind | default "Issuer" }}
{{- end }}
