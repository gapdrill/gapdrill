{{- define "demo.fullname" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "demo.labels" -}}
app.kubernetes.io/name: demo
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
