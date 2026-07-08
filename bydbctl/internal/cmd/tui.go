// Licensed to Apache Software Foundation (ASF) under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Apache Software Foundation (ASF) licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent/acp"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent/codex"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/agent/fake"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/applog"
	tuiapp "github.com/apache/skywalking-banyandb/bydbctl/internal/tui/app"
	"github.com/apache/skywalking-banyandb/bydbctl/internal/tui/tools"
	"github.com/apache/skywalking-banyandb/pkg/version"
)

const (
	agentProviderFake      = "fake"
	agentProviderCodexExec = "codex-exec"
	agentProviderACP       = "acp"
	agentProviderCodexACP  = "codex-acp"
)

func newAgentCmd() *cobra.Command {
	var agentProvider string
	var codexBin string
	var acpCommand string
	var acpArgs []string
	var mcpConfig string
	var initialGoal string
	var initialResourceType string
	var initialResourceName string
	var initialGroups string
	var initialStart string
	var initialEnd string
	var maxRetries int
	var logDir string
	agentCmd := &cobra.Command{
		Use:     "agent",
		Version: version.Build(),
		Short:   "Open the interactive BYDBQL agent TUI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if initialGroups == "" {
				initialGroups = viper.GetString("group")
			}
			workingDirectory, wdErr := os.Getwd()
			if wdErr != nil {
				return fmt.Errorf("failed to get working directory: %w", wdErr)
			}
			agentGateway, gatewayErr := newAgentGateway(agentProvider, codexBin, acpCommand, acpArgs, mcpConfig, workingDirectory)
			if gatewayErr != nil {
				return gatewayErr
			}
			sessionLog, logErr := applog.New(logDir)
			if logErr != nil {
				return fmt.Errorf("failed to create agent session log: %w", logErr)
			}
			defer func() {
				_ = sessionLog.Close()
			}()
			model := tuiapp.NewModel(tuiapp.Config{
				AgentGateway: agentGateway,
				Executor: tools.NewHTTPExecutor(tools.HTTPConfig{
					Addr:     viper.GetString("addr"),
					Username: viper.GetString("username"),
					Password: viper.GetString("password"),
				}),
				SessionLog:   sessionLog,
				Provider:     agentProvider,
				Goal:         initialGoal,
				ResourceType: initialResourceType,
				ResourceName: initialResourceName,
				Groups:       initialGroups,
				Start:        initialStart,
				End:          initialEnd,
				MaxRetries:   maxRetries,
			})
			program := tea.NewProgram(model, tea.WithAltScreen())
			if _, runErr := program.Run(); runErr != nil {
				return fmt.Errorf("failed to run agent TUI: %w", runErr)
			}
			fmt.Fprintf(os.Stderr, "agent session log: %s\n", sessionLog.Path())
			return nil
		},
	}
	agentCmd.Flags().StringVar(&agentProvider, "agent", agentProviderFake, "agent adapter: fake, codex-exec, acp, or codex-acp")
	agentCmd.Flags().StringVar(&codexBin, "codex-bin", "codex", "codex executable used by --agent codex-exec")
	agentCmd.Flags().StringVar(&acpCommand, "acp-command", "", "ACP-compatible stdio command used by --agent acp")
	agentCmd.Flags().StringArrayVar(&acpArgs, "acp-arg", nil, "argument passed to --acp-command; may be repeated")
	agentCmd.Flags().StringVar(&mcpConfig, "mcp-config", "", "MCP config file passed to ACP agents; empty disables MCP injection")
	agentCmd.Flags().StringVar(&initialGoal, "goal", "", "initial natural language query goal")
	agentCmd.Flags().StringVar(&initialResourceType, "resource-type", "MEASURE", "initial resource type: MEASURE, STREAM, TRACE, PROPERTY, or TOPN")
	agentCmd.Flags().StringVar(&initialResourceName, "name", "", "initial resource name")
	agentCmd.Flags().StringVar(&initialGroups, "groups", "", "initial group list")
	agentCmd.Flags().StringVar(&initialStart, "start", "-30m", "initial BYDBQL time start")
	agentCmd.Flags().StringVar(&initialEnd, "end", "", "initial BYDBQL time end")
	agentCmd.Flags().IntVar(&maxRetries, "agent-retries", 2, "maximum agent repair retries after validation errors")
	agentCmd.Flags().StringVar(&logDir, "log-dir", "", "directory for agent session logs; default is $HOME/.bydbctl/logs")
	return agentCmd
}

func newAgentGateway(provider, codexBin, acpCommand string, acpArgs []string, mcpConfig string, workingDirectory string) (agent.Gateway, error) {
	switch provider {
	case agentProviderFake:
		return fake.NewGateway(), nil
	case agentProviderCodexExec:
		return codex.NewExecGateway(
			codex.WithBinPath(codexBin),
			codex.WithWorkingDirectory(workingDirectory),
		), nil
	case agentProviderACP:
		mcpServers, mcpErr := loadMCPServers(mcpConfig, workingDirectory)
		if mcpErr != nil {
			return nil, mcpErr
		}
		return acp.NewGateway(acpCommand, acpArgs...).WithWorkingDirectory(workingDirectory).WithMCPServers(mcpServers), nil
	case agentProviderCodexACP:
		mcpServers, mcpErr := loadMCPServers(mcpConfig, workingDirectory)
		if mcpErr != nil {
			return nil, mcpErr
		}
		return acp.NewGateway("npx", "-y", "@agentclientprotocol/codex-acp").WithWorkingDirectory(workingDirectory).WithMCPServers(mcpServers), nil
	default:
		return nil, fmt.Errorf("unsupported agent provider %q", provider)
	}
}

func loadMCPServers(configPath, workingDirectory string) (any, error) {
	if configPath == "" {
		return nil, nil
	}
	resolvedPath := configPath
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(workingDirectory, resolvedPath)
	}
	configBytes, readErr := os.ReadFile(resolvedPath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read MCP config %s: %w", resolvedPath, readErr)
	}
	var rawConfig map[string]any
	if unmarshalErr := json.Unmarshal(configBytes, &rawConfig); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to parse MCP config %s: %w", resolvedPath, unmarshalErr)
	}
	mcpServers, ok := rawConfig["mcpServers"]
	if !ok {
		return nil, nil
	}
	return normalizeMCPServers(mcpServers), nil
}

func normalizeMCPServers(value any) any {
	switch typedValue := value.(type) {
	case []any:
		return typedValue
	case map[string]any:
		servers := make([]any, 0, len(typedValue))
		for name, serverValue := range typedValue {
			server, serverOK := serverValue.(map[string]any)
			if !serverOK {
				continue
			}
			serverCopy := make(map[string]any, len(server)+1)
			for serverKey, configValue := range server {
				serverCopy[serverKey] = configValue
			}
			if _, hasName := serverCopy["name"]; !hasName {
				serverCopy["name"] = name
			}
			servers = append(servers, serverCopy)
		}
		return servers
	default:
		return []any{}
	}
}
