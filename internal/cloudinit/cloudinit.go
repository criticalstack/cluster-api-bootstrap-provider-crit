/*
Copyright 2020 Critical Stack, LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cloudinit

import (
	"bytes"
	"strings"
	"text/template"

	"github.com/pkg/errors"

	bootstrapv1 "github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/bootstrap/v1alpha2"
)

const (
	nodeCloudInit = `## template: jinja
#cloud-config
{{template "files" .Files}}
runcmd:
{{- template "commands" .PreCritCommands }}
  - 'crit up --config /var/lib/crit/config.yaml {{.Verbosity}}'
{{- template "commands" .PostCritCommands }}
{{- template "ntp" .NTP }}
{{- template "users" .Users }}
`
	commandsTemplate = `{{- define "commands" -}}
{{ range . }}
  - {{printf "%q" .}}
{{- end -}}
{{- end -}}
`
	filesTemplate = `{{ define "files" -}}
write_files:{{ range . }}
-   path: {{.Path}}
    {{ if ne .Encoding "" -}}
    encoding: "{{.Encoding}}"
    {{ end -}}
    {{ if ne .Owner "" -}}
    owner: {{.Owner}}
    {{ end -}}
    {{ if ne .Permissions "" -}}
    permissions: '{{.Permissions}}'
    {{ end -}}
    content: |
{{.Content | Indent 6}}
{{- end -}}
{{- end -}}
`
	ntpTemplate = `{{ define "ntp" -}}
{{- if . }}
ntp:
  {{ if .Enabled -}}
  enabled: true
  {{ end -}}
  servers:{{ range .Servers }}
    - {{ . }}
  {{- end -}}
{{- end -}}
{{- end -}}
`
	usersTemplate = `{{ define "users" -}}
{{- if . }}
users:{{ range . }}
  - name: {{ .Name }}
    {{- if .Passwd }}
    passwd: {{ .Passwd }}
    {{- end -}}
    {{- if .Gecos }}
    gecos: {{ .Gecos }}
    {{- end -}}
    {{- if .Groups }}
    groups: {{ .Groups }}
    {{- end -}}
    {{- if .HomeDir }}
    homedir: {{ .HomeDir }}
    {{- end -}}
    {{- if .Inactive }}
    inactive: true
    {{- end -}}
    {{- if .LockPassword }}
    lock_passwd: {{ .LockPassword }}
    {{- end -}}
    {{- if .Shell }}
    shell: {{ .Shell }}
    {{- end -}}
    {{- if .PrimaryGroup }}
    primary_group: {{ .PrimaryGroup }}
    {{- end -}}
    {{- if .Sudo }}
    sudo: {{ .Sudo }}
    {{- end -}}
    {{- if .SSHAuthorizedKeys }}
    ssh_authorized_keys:{{ range .SSHAuthorizedKeys }}
      - {{ . }}
    {{- end -}}
    {{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
`
)

type Config struct {
	Files            []bootstrapv1.File
	PreCritCommands  []string
	PostCritCommands []string
	Users            []bootstrapv1.User
	NTP              *bootstrapv1.NTP
	Verbosity        string
}

func Write(cfg *Config) ([]byte, error) {
	tm := template.New("Node").Funcs(template.FuncMap{
		"trim": strings.TrimSpace,
		"Indent": func(i int, input string) string {
			split := strings.Split(input, "\n")
			ident := "\n" + strings.Repeat(" ", i)
			return strings.Repeat(" ", i) + strings.Join(split, ident)
		},
	})
	if _, err := tm.Parse(filesTemplate); err != nil {
		return nil, errors.Wrap(err, "failed to parse files template")
	}
	if _, err := tm.Parse(commandsTemplate); err != nil {
		return nil, errors.Wrap(err, "failed to parse commands template")
	}
	if _, err := tm.Parse(ntpTemplate); err != nil {
		return nil, errors.Wrap(err, "failed to parse ntp template")
	}
	if _, err := tm.Parse(usersTemplate); err != nil {
		return nil, errors.Wrap(err, "failed to parse users template")
	}
	t, err := tm.Parse(nodeCloudInit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse Node template")
	}

	var out bytes.Buffer
	if err := t.Execute(&out, cfg); err != nil {
		return nil, errors.Wrap(err, "failed to generate Node template")
	}
	return out.Bytes(), nil
}
