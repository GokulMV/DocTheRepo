{{- define "dth.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "dth.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "dth.selector" -}}
app.kubernetes.io/name: {{ .root.Chart.Name }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .role }}
{{- end -}}

{{- define "dth.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "dth.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "dth.databaseSecret" -}}
{{- default (printf "%s-database" (include "dth.fullname" .)) .Values.database.existingSecret -}}
{{- end -}}

{{/* Fail fast on settings that would deploy an insecure or broken Hub. */}}
{{- define "dth.validate" -}}
{{- if and (not .Values.database.existingSecret) (not .Values.database.url) -}}
{{- fail "set database.existingSecret (recommended) or database.url" -}}
{{- end -}}
{{- if not (has .Values.secrets.provider (list "awskms" "gcpkms" "localfile")) -}}
{{- fail "secrets.provider must be awskms, gcpkms, or localfile" -}}
{{- end -}}
{{- if and (ne .Values.secrets.provider "localfile") (not .Values.secrets.kmsKeyId) -}}
{{- fail "secrets.kmsKeyId is required for awskms/gcpkms" -}}
{{- end -}}
{{- if and (eq .Values.secrets.provider "localfile") (not .Values.secrets.localKey.existingSecret) -}}
{{- fail "secrets.localKey.existingSecret is required with provider localfile (every pod must share one key)" -}}
{{- end -}}
{{- if eq .Values.auth.mode "oidc" -}}
{{- if or (not .Values.auth.oidc.issuer) (not .Values.auth.oidc.clientId) (not .Values.auth.oidc.existingSecret) -}}
{{- fail "auth.mode=oidc needs auth.oidc.issuer, clientId, and existingSecret" -}}
{{- end -}}
{{- if not .Values.auth.oidc.allowedDomains -}}
{{- fail "auth.oidc.allowedDomains is required: without it any account at the IdP could sign in, and the first becomes owner" -}}
{{- end -}}
{{- else if ne .Values.auth.mode "local" -}}
{{- fail "auth.mode must be oidc or local" -}}
{{- end -}}
{{- end -}}

{{- define "dth.env" -}}
- name: DTH_PUBLIC_URL
  value: {{ .Values.publicURL | quote }}
- name: DTH_LISTEN
  value: "0.0.0.0:8080"
- name: DTH_METRICS_LISTEN
  value: "0.0.0.0:9090"
- name: DTH_LOG_FORMAT
  value: json
- name: DTH_LOG_LEVEL
  value: {{ .Values.logLevel | quote }}
- name: DTH_DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ include "dth.databaseSecret" . }}
      key: {{ .Values.database.key }}
- name: DTH_SECRETS_PROVIDER
  value: {{ .Values.secrets.provider }}
{{- if eq .Values.secrets.provider "localfile" }}
- name: DTH_LOCAL_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.secrets.localKey.existingSecret }}
      key: {{ .Values.secrets.localKey.key }}
{{- else }}
- name: DTH_KMS_KEY_ID
  value: {{ .Values.secrets.kmsKeyId | quote }}
{{- end }}
- name: DTH_AUTH_MODE
  value: {{ .Values.auth.mode }}
{{- if .Values.email.existingSecret }}
- name: DTH_EMAIL_FROM
  value: {{ required "email.from is required with email.existingSecret" .Values.email.from | quote }}
- name: DTH_SMTP_URL
  valueFrom:
    secretKeyRef:
      name: {{ .Values.email.existingSecret }}
      key: {{ .Values.email.key }}
{{- end }}
{{- with .Values.auth.ownerEmail }}
- name: DTH_OWNER_EMAIL
  value: {{ . | quote }}
{{- end }}
{{- if eq .Values.auth.mode "oidc" }}
- name: DTH_OIDC_ISSUER
  value: {{ .Values.auth.oidc.issuer | quote }}
- name: DTH_OIDC_CLIENT_ID
  value: {{ .Values.auth.oidc.clientId | quote }}
- name: DTH_OIDC_REDIRECT_URL
  value: {{ printf "%s/api/v1/auth/callback" (trimSuffix "/" .Values.publicURL) | quote }}
- name: DTH_OIDC_ALLOWED_DOMAINS
  value: {{ join "," .Values.auth.oidc.allowedDomains | quote }}
- name: DTH_OIDC_CLIENT_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ .Values.auth.oidc.existingSecret }}
      key: {{ .Values.auth.oidc.key }}
{{- else if .Values.auth.local.existingSecret }}
- name: DTH_OWNER_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ .Values.auth.local.existingSecret }}
      key: {{ .Values.auth.local.key }}
{{- end }}
{{- if or .Values.settings.existingSecret .Values.settings.inline }}
- name: DTH_SETTINGS
  valueFrom:
    secretKeyRef:
      name: {{ default (printf "%s-settings" (include "dth.fullname" .)) .Values.settings.existingSecret }}
      key: {{ .Values.settings.key }}
{{- end }}
{{- with .Values.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}
