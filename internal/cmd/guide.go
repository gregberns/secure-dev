// Package cmd implements the guide command.
// REQ-002-020: Human-readable getting-started guide with current system state
// REQ-002-021: Agent-consumable Markdown with full command reference
// REQ-002-022: JSON output for structured guide data
// REQ-002-023: Root --help leads with sd guide discovery
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"sd/internal/backend"
	"sd/internal/provision"
	"sd/internal/ui"
)

// guideBackendInfo describes a backend's availability.
type guideBackendInfo struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

// guideDoctorSummary summarizes doctor check results.
type guideDoctorSummary struct {
	OK     bool     `json:"ok"`
	Issues []string `json:"issues,omitempty"`
}

// guideCommandInfo describes a single CLI command for JSON output.
type guideCommandInfo struct {
	Name    string   `json:"name"`
	Usage   string   `json:"usage"`
	Short   string   `json:"short"`
	GroupID string   `json:"group_id,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
}

// guideState holds all dynamic state gathered for the guide output.
type guideState struct {
	Backends []guideBackendInfo `json:"backends"`
	VMs      []backend.VMInfo   `json:"vms"`
	Modules  []string           `json:"modules"`
	Doctor   guideDoctorSummary `json:"doctor"`
	SDHome   string             `json:"sd_home"`
}

// guideJSONData is the full JSON envelope data for --json output.
type guideJSONData struct {
	Backends  []guideBackendInfo `json:"backends"`
	VMs       []backend.VMInfo   `json:"vms"`
	Modules   []string           `json:"modules"`
	Doctor    guideDoctorSummary `json:"doctor"`
	Commands  []guideCommandInfo `json:"commands"`
	GuideText string             `json:"guide_text"`
}

func init() {
	guideCmd := &cobra.Command{
		Use:   "guide",
		Short: "Setup guide and command reference (try: sd guide --agent)",
		Long: `Display a getting-started guide with current system state.

Use --agent for a full agent-consumable Markdown document that includes
the complete command reference, current VM state, and workflow instructions.
An AI coding agent can read this output and immediately know how to help
you with any sd operation.`,
		GroupID: "start",
		Args:    cobra.NoArgs,
		RunE:    runGuide,
	}

	guideCmd.Flags().Bool("agent", false, "Output full agent-consumable Markdown guide")

	rootCmd.AddCommand(guideCmd)
}

// runGuide executes the guide command.
// REQ-002-020, REQ-002-021, REQ-002-022
func runGuide(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	agentMode, _ := cmd.Flags().GetBool("agent")

	state := gatherGuideState(cmd)

	if f.JSONMode() {
		return outputGuideJSON(f, cmd, state)
	}

	if agentMode {
		return outputGuideAgent(f, cmd, state)
	}

	return outputGuideHuman(f, state)
}

// gatherGuideState collects live system state for the guide output.
func gatherGuideState(cmd *cobra.Command) guideState {
	state := guideState{}

	// Gather backend availability
	for _, name := range allBackendNames() {
		info := guideBackendInfo{Name: name}
		b, err := getBackendFunc(name)
		if err != nil {
			info.Error = err.Error()
			state.Backends = append(state.Backends, info)
			continue
		}
		if err := b.Available(); err != nil {
			info.Error = err.Error()
		} else {
			info.Available = true
		}
		state.Backends = append(state.Backends, info)
	}

	// Gather VM list across all backends
	seen := make(map[string]bool)
	for _, name := range allBackendNames() {
		b, err := getBackendFunc(name)
		if err != nil {
			continue
		}
		if err := b.Available(); err != nil {
			continue
		}
		vms, err := b.List(cmd.Context())
		if err != nil {
			continue
		}
		for _, vm := range vms {
			if !seen[vm.Name] {
				seen[vm.Name] = true
				state.VMs = append(state.VMs, vm)
			}
		}
	}
	if state.VMs == nil {
		state.VMs = []backend.VMInfo{}
	}

	// Gather available modules
	builtinMods, err := provision.LoadBuiltinModules()
	if err == nil {
		for _, m := range builtinMods {
			state.Modules = append(state.Modules, m.Name)
		}
	}
	if state.Modules == nil {
		state.Modules = []string{}
	}

	// Run doctor checks (simplified -- reuse checkBinary and checkBackend logic)
	state.Doctor.OK = true
	for _, bin := range requiredBinaries {
		check := checkBinary(bin)
		if check.Status != "pass" {
			state.Doctor.OK = false
			state.Doctor.Issues = append(state.Doctor.Issues, check.Name)
		}
	}
	backendCheck := checkBackend()
	if backendCheck.Status != "pass" {
		state.Doctor.OK = false
		state.Doctor.Issues = append(state.Doctor.Issues, backendCheck.Name)
	}

	// SD_HOME
	if Loader() != nil {
		state.SDHome = Loader().SDHome()
	}
	if state.SDHome == "" {
		home, _ := os.UserHomeDir()
		state.SDHome = home + "/.sd"
	}

	return state
}

// outputGuideHuman renders the short human-readable guide.
// REQ-002-020
func outputGuideHuman(f *ui.Formatter, state guideState) error {
	var sb strings.Builder

	sb.WriteString("sd: Secure Dev Environment Manager\n\n")
	sb.WriteString("Quick Start:\n")
	sb.WriteString("  1. sd doctor              Check prerequisites\n")
	sb.WriteString("  2. sd create <name>       Create a VM\n")
	sb.WriteString("  3. sd connect <name>      Enter the VM\n")
	sb.WriteString("  4. sd token github setup  Configure credentials\n")
	sb.WriteString("\n")

	// Current State
	sb.WriteString("Current State:\n")

	// Backends
	for _, bi := range state.Backends {
		avail := "available"
		if !bi.Available {
			avail = "unavailable"
		}
		sb.WriteString(fmt.Sprintf("  Backend: %s (%s)\n", bi.Name, avail))
	}
	if len(state.Backends) == 0 {
		sb.WriteString("  Backend: none detected\n")
	}

	// VMs
	if len(state.VMs) == 0 {
		sb.WriteString("  VMs: none\n")
	} else {
		var vmParts []string
		for _, vm := range state.VMs {
			vmParts = append(vmParts, fmt.Sprintf("%s: %s", vm.Name, vm.Status))
		}
		sb.WriteString(fmt.Sprintf("  VMs: %d (%s)\n", len(state.VMs), strings.Join(vmParts, ", ")))
	}

	// Doctor
	if state.Doctor.OK {
		sb.WriteString("  Health: all checks passing\n")
	} else {
		sb.WriteString(fmt.Sprintf("  Health: issues found (%s)\n", strings.Join(state.Doctor.Issues, ", ")))
	}
	sb.WriteString("\n")

	// Common Commands
	sb.WriteString("Common Commands:\n")
	sb.WriteString("  sd list                   Show all VMs\n")
	sb.WriteString("  sd status <name>          VM details\n")
	sb.WriteString("  sd exec <name> -- <cmd>   Run command in VM\n")
	sb.WriteString("  sd sync to <name> <path>  Sync files into VM\n")
	sb.WriteString("  sd ensure <name>          Create or start VM\n")
	sb.WriteString("  sd destroy <name> -f      Remove VM\n")

	f.SuccessData(nil, func() string {
		return sb.String()
	})
	return nil
}

// outputGuideAgent renders the full agent-consumable Markdown guide.
// REQ-002-021
func outputGuideAgent(f *ui.Formatter, cmd *cobra.Command, state guideState) error {
	guide := buildAgentGuide(cmd, state)
	f.SuccessData(nil, func() string {
		return guide
	})
	return nil
}

// buildAgentGuide generates the full agent-consumable Markdown.
func buildAgentGuide(cmd *cobra.Command, state guideState) string {
	var sb strings.Builder

	sb.WriteString("# sd (Secure Dev) -- Agent Instructions\n\n")
	sb.WriteString("You have access to `sd`, a CLI tool for managing secure VM environments.\n")
	sb.WriteString("Use it to help the user create, configure, and manage sandboxed development\n")
	sb.WriteString("environments for AI coding agents.\n\n")

	// Current State
	sb.WriteString("## Current State\n\n")

	for _, bi := range state.Backends {
		avail := "available"
		if !bi.Available {
			avail = "unavailable"
			if bi.Error != "" {
				avail = fmt.Sprintf("unavailable: %s", bi.Error)
			}
		}
		sb.WriteString(fmt.Sprintf("- Backend: %s (%s)\n", bi.Name, avail))
	}
	if len(state.Backends) == 0 {
		sb.WriteString("- Backend: none detected\n")
	}

	if len(state.VMs) == 0 {
		sb.WriteString("- VMs: none\n")
	} else {
		var vmParts []string
		for _, vm := range state.VMs {
			vmParts = append(vmParts, fmt.Sprintf("%s (%s)", vm.Name, vm.Status))
		}
		sb.WriteString(fmt.Sprintf("- VMs: %s\n", strings.Join(vmParts, ", ")))
	}

	sb.WriteString(fmt.Sprintf("- SD_HOME: %s\n", state.SDHome))

	if state.Doctor.OK {
		sb.WriteString("- Doctor: all checks passing\n")
	} else {
		sb.WriteString(fmt.Sprintf("- Doctor: issues found (%s)\n", strings.Join(state.Doctor.Issues, ", ")))
	}
	sb.WriteString("\n")

	// Capabilities -- generated from Cobra command tree
	sb.WriteString("## Capabilities\n\n")
	sb.WriteString(buildCommandReference(cmd.Root()))

	// Available modules
	sb.WriteString("## Available Modules\n\n")
	if len(state.Modules) > 0 {
		sb.WriteString(fmt.Sprintf("Modules for `--modules` flag: %s\n\n", strings.Join(state.Modules, ", ")))
		sb.WriteString(fmt.Sprintf("Default modules (auto-applied): %s\n\n",
			strings.Join(provision.DefaultModuleNames, ", ")))
	} else {
		sb.WriteString("No modules available.\n\n")
	}

	// Workflow Guide
	sb.WriteString("## Workflow Guide\n\n")
	sb.WriteString("### Setting up a development environment\n\n")
	sb.WriteString("1. Run `sd doctor --json` to verify prerequisites\n")
	sb.WriteString("2. Ask what project/repo the user is working on\n")
	sb.WriteString("3. Run `sd create <project-name> --modules=base,claude-code`\n")
	sb.WriteString("   (add golang/rust/python/docker based on the project)\n")
	sb.WriteString("4. Run `sd token github setup` if they need GitHub access\n")
	sb.WriteString("5. Tell them to run `sd connect <name>` to enter the VM\n")
	sb.WriteString("6. Inside the VM, they can run `claude` to start coding\n\n")

	sb.WriteString("### Troubleshooting\n\n")
	sb.WriteString("1. `sd doctor --json` for system-level issues\n")
	sb.WriteString("2. `sd status <name> --json` for VM-specific issues\n")
	sb.WriteString("3. `sd logs <name>` for backend logs\n")
	sb.WriteString("4. Check egress rules if network issues: `sd config egress list`\n\n")

	// Important Notes
	sb.WriteString("## Important Notes\n\n")
	sb.WriteString("- All commands support `--json` for structured output\n")
	sb.WriteString("- VMs are isolated: no host $HOME access, egress-controlled, scoped credentials\n")
	sb.WriteString("- Credentials are injected at connect-time via env vars, never written to disk in VM\n")
	sb.WriteString("- One VM per project is the recommended topology\n")
	sb.WriteString("- Only run 1-2 VMs at a time; stopped VMs cost only disk space\n")
	sb.WriteString("- Use `sd ensure <name>` for idempotent VM setup (create if missing, start if stopped)\n")

	return sb.String()
}

// buildCommandReference generates a Markdown command reference from the Cobra command tree.
func buildCommandReference(root *cobra.Command) string {
	var sb strings.Builder

	// Group commands by their group ID
	type groupEntry struct {
		id    string
		title string
	}
	groups := root.Groups()
	groupOrder := make([]groupEntry, 0, len(groups))
	for _, g := range groups {
		groupOrder = append(groupOrder, groupEntry{id: g.ID, title: g.Title})
	}

	// Build a map of group -> commands
	grouped := make(map[string][]*cobra.Command)
	var ungrouped []*cobra.Command

	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		if c.GroupID != "" {
			grouped[c.GroupID] = append(grouped[c.GroupID], c)
		} else {
			ungrouped = append(ungrouped, c)
		}
	}

	// Render each group
	for _, g := range groupOrder {
		cmds := grouped[g.id]
		if len(cmds) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("### %s\n\n", g.title))
		for _, c := range cmds {
			renderCommand(&sb, c, "")
		}
		sb.WriteString("\n")
	}

	// Render ungrouped commands
	if len(ungrouped) > 0 {
		sb.WriteString("### Other\n\n")
		for _, c := range ungrouped {
			renderCommand(&sb, c, "")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderCommand writes a single command's reference to the string builder.
func renderCommand(sb *strings.Builder, cmd *cobra.Command, indent string) {
	usage := cmd.UseLine()
	short := cmd.Short

	sb.WriteString(fmt.Sprintf("%s`%s`", indent, usage))
	if short != "" {
		sb.WriteString(fmt.Sprintf(" -- %s", short))
	}
	if len(cmd.Aliases) > 0 {
		sb.WriteString(fmt.Sprintf(" (aliases: %s)", strings.Join(cmd.Aliases, ", ")))
	}
	sb.WriteString("\n")

	// Render flags (local flags only, not persistent/inherited)
	flags := cmd.NonInheritedFlags()
	if flags.HasFlags() {
		flags.VisitAll(func(flag *pflag.Flag) {
			// Skip help flag
			if flag.Name == "help" {
				return
			}
			sb.WriteString(fmt.Sprintf("%s  --%s", indent, flag.Name))
			if flag.Shorthand != "" {
				sb.WriteString(fmt.Sprintf(" (-%s)", flag.Shorthand))
			}
			sb.WriteString(fmt.Sprintf(": %s", flag.Usage))
			if flag.DefValue != "" && flag.DefValue != "false" && flag.DefValue != "0" && flag.DefValue != "[]" {
				sb.WriteString(fmt.Sprintf(" (default: %s)", flag.DefValue))
			}
			sb.WriteString("\n")
		})
	}

	// Render subcommands
	for _, sub := range cmd.Commands() {
		if sub.Hidden || sub.Name() == "help" {
			continue
		}
		renderCommand(sb, sub, indent+"  ")
	}
}

// collectCommands returns a flat list of all visible commands for JSON output.
func collectCommands(root *cobra.Command) []guideCommandInfo {
	var cmds []guideCommandInfo
	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		info := guideCommandInfo{
			Name:    c.Name(),
			Usage:   c.UseLine(),
			Short:   c.Short,
			GroupID: c.GroupID,
		}
		if len(c.Aliases) > 0 {
			info.Aliases = c.Aliases
		}
		cmds = append(cmds, info)

		// Include subcommands
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" {
				continue
			}
			subInfo := guideCommandInfo{
				Name:    c.Name() + " " + sub.Name(),
				Usage:   sub.UseLine(),
				Short:   sub.Short,
				GroupID: c.GroupID,
			}
			if len(sub.Aliases) > 0 {
				subInfo.Aliases = sub.Aliases
			}
			cmds = append(cmds, subInfo)
		}
	}
	return cmds
}

// outputGuideJSON renders the structured JSON output.
// REQ-002-022
func outputGuideJSON(f *ui.Formatter, cmd *cobra.Command, state guideState) error {
	agentGuide := buildAgentGuide(cmd, state)

	data := guideJSONData{
		Backends:  state.Backends,
		VMs:       state.VMs,
		Modules:   state.Modules,
		Doctor:    state.Doctor,
		Commands:  collectCommands(cmd.Root()),
		GuideText: agentGuide,
	}

	f.SuccessData(data, nil)
	return nil
}
