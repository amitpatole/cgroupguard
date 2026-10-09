{{- define "cgroupguard.name" -}}cgroupguard{{- end -}}
{{- define "cgroupguard.fullname" -}}{{ printf "%s-%s" .Release.Name (include "cgroupguard.name" .) | trunc 63 | trimSuffix "-" }}{{- end -}}
{{- define "cgroupguard.labels" -}}
app.kubernetes.io/name: {{ include "cgroupguard.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
{{- end -}}
{{- define "cgroupguard.selector" -}}
app.kubernetes.io/name: {{ include "cgroupguard.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
{{- define "cgroupguard.image" -}}
{{- $repo := required "image.repository is required: there is no default registry" .Values.image.repository -}}
{{- if .Values.image.digest -}}{{ printf "%s@%s" $repo .Values.image.digest }}{{- else -}}{{ printf "%s:%s" $repo (default .Chart.AppVersion .Values.image.tag) }}{{- end -}}
{{- end -}}
