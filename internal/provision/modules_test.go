package provision

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// --- LoadBuiltinModules Tests ---
// REQ-006-014: Embedded modules load correctly from //go:embed.
// REQ-006-001: Built-in module set.

func TestLoadBuiltinModules_AllPresent(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	names := make([]string, len(modules))
	for i, m := range modules {
		names[i] = m.Name
	}
	assert.Equal(t, BuiltinModuleNames, names, "all 10 built-in modules must be present in canonical order")
}

func TestLoadBuiltinModules_Count(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	assert.Len(t, modules, 10, "REQ-006-001 specifies exactly 10 built-in modules")
}

func TestLoadBuiltinModules_AllValid(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	for _, m := range modules {
		err := m.Validate()
		assert.NoError(t, err, "module %q should validate successfully", m.Name)
	}
}

func TestLoadBuiltinModules_AllHaveDescriptions(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	for _, m := range modules {
		assert.NotEmpty(t, m.Description, "module %q must have a description", m.Name)
	}
}

func TestLoadBuiltinModules_AllHaveScripts(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	for _, m := range modules {
		assert.NotEmpty(t, m.Scripts, "module %q must have at least one script", m.Name)
	}
}

func TestLoadBuiltinModules_BaseHasNoDependencies(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	base := modules[0]
	assert.Equal(t, "base", base.Name)
	assert.Empty(t, base.DependsOn, "base module has no dependencies")
}

func TestLoadBuiltinModules_NonBaseDependsOnBase(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	for _, m := range modules {
		if m.Name == "base" {
			continue
		}
		assert.Contains(t, m.DependsOn, "base",
			"module %q must depend on base (REQ-006-002)", m.Name)
	}
}

func TestLoadBuiltinModules_BaseInstallsRequiredPackages(t *testing.T) {
	// REQ-006-001: base module installs git, curl, build-essential, ca-certificates, jq, tmux, vim
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	base := modules[0]
	assert.Equal(t, "base", base.Name)

	// All scripts combined should mention each required package
	allScripts := ""
	for _, s := range base.Scripts {
		allScripts += s.Script + " "
	}

	requiredPackages := []string{"git", "curl", "build-essential", "ca-certificates", "jq", "tmux", "vim"}
	for _, pkg := range requiredPackages {
		assert.Contains(t, allScripts, pkg,
			"base module must install %q (REQ-006-001)", pkg)
	}
}

func TestLoadBuiltinModules_BaseHasGitCredentialPrevention(t *testing.T) {
	// REQ-004-030: base module must prevent git credential caching to disk
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	base := modules[0]
	assert.Equal(t, "base", base.Name)

	// Must have a user-mode script for git credential prevention
	found := false
	for _, s := range base.Scripts {
		if s.Mode == ModeUser {
			assert.Contains(t, s.Script, "credential.helper",
				"base user script must configure credential.helper (REQ-004-030)")
			assert.Contains(t, s.Script, ".git-credentials",
				"base user script must check for ~/.git-credentials (REQ-004-030)")
			found = true
		}
	}
	assert.True(t, found, "base module must have a user-mode script (REQ-004-030)")
}

func TestLoadBuiltinModules_BaseHasMultipleScripts(t *testing.T) {
	// REQ-004-030: base module now has 2 scripts (system + user)
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	base := modules[0]
	assert.Equal(t, "base", base.Name)
	assert.Len(t, base.Scripts, 2, "base module should have system and user scripts")

	// First is system (apt-get), second is user (git config)
	assert.Equal(t, ModeSystem, base.Scripts[0].Mode)
	assert.Equal(t, ModeUser, base.Scripts[1].Mode)
}

func TestLoadBuiltinModules_ClaudeCodeInstallsNodeAndClaude(t *testing.T) {
	// REQ-006-011: claude-code module installs Node.js via nvm and Claude Code CLI
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	var claudeCode *Module
	for i := range modules {
		if modules[i].Name == "claude-code" {
			claudeCode = &modules[i]
			break
		}
	}
	require.NotNil(t, claudeCode)

	allScripts := ""
	for _, s := range claudeCode.Scripts {
		allScripts += s.Script + " "
	}

	assert.Contains(t, allScripts, "nvm", "claude-code must install Node.js via nvm")
	assert.Contains(t, allScripts, "claude-code", "claude-code must install Claude Code CLI")
}

func TestLoadBuiltinModules_GolangHasChecksums(t *testing.T) {
	// REQ-006-016: modules that download must have checksums
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	var golang *Module
	for i := range modules {
		if modules[i].Name == "golang" {
			golang = &modules[i]
			break
		}
	}
	require.NotNil(t, golang)
	assert.NotEmpty(t, golang.Checksums, "golang module must have checksums (REQ-006-016)")
}

func TestLoadBuiltinModules_DownloadModulesHaveChecksums(t *testing.T) {
	// REQ-006-016: Built-in modules that download binaries (tar.gz, .sh, etc.)
	// MUST include checksums. System package downloads (apt) are excluded.
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	// Modules that download standalone binaries/archives
	requiresChecksums := map[string]bool{
		"golang":      true,
		"claude-code": true,
		"rust":        true,
		"github-cli":  true,
	}

	for _, m := range modules {
		if requiresChecksums[m.Name] {
			assert.NotEmpty(t, m.Checksums,
				"module %q downloads binaries and must have checksums (REQ-006-016)", m.Name)
		}
	}
}

func TestLoadBuiltinModules_NoCurlPipeSh(t *testing.T) {
	// REQ-006-016: No built-in module uses curl | sh patterns
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	for _, m := range modules {
		for i, s := range m.Scripts {
			// Check for dangerous pipe patterns
			assert.NotContains(t, s.Script, "curl | sh",
				"module %q scripts[%d]: curl | sh is forbidden (REQ-006-016)", m.Name, i)
			assert.NotContains(t, s.Script, "curl | bash",
				"module %q scripts[%d]: curl | bash is forbidden (REQ-006-016)", m.Name, i)
		}
	}
}

func TestLoadBuiltinModules_AllProbesPresent(t *testing.T) {
	// REQ-006-008: Modules should have readiness probes
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	for _, m := range modules {
		assert.NotNil(t, m.Probe, "module %q should have a readiness probe", m.Name)
		if m.Probe != nil {
			assert.NotEmpty(t, m.Probe.Command, "module %q probe must have a command", m.Name)
		}
	}
}

func TestLoadBuiltinModules_ProbeDefaultsApplied(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	for _, m := range modules {
		if m.Probe != nil {
			m.ApplyProbeDefaults()
			assert.NotZero(t, m.Probe.Interval,
				"module %q probe interval should be non-zero after defaults", m.Name)
			assert.NotZero(t, m.Probe.Timeout,
				"module %q probe timeout should be non-zero after defaults", m.Name)
		}
	}
}

func TestLoadBuiltinModules_AllIdempotent(t *testing.T) {
	// REQ-006-006: Scripts should use command -v guards for idempotency
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	for _, m := range modules {
		if m.Name == "base" {
			// base always runs apt-get which is naturally idempotent
			continue
		}
		allScripts := ""
		for _, s := range m.Scripts {
			allScripts += s.Script + " "
		}
		// At least one script should check if tool is already installed
		assert.True(t,
			strings.Contains(allScripts, "command -v") || strings.Contains(allScripts, "already installed"),
			"module %q should check for existing installation (REQ-006-006)", m.Name)
	}
}

func TestLoadBuiltinModules_SshHardeningConfiguresPortForwarding(t *testing.T) {
	// REQ-004-026: SSH port forwarding restrictions provisioned as a module
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	var sshHardening *Module
	for i := range modules {
		if modules[i].Name == "ssh-hardening" {
			sshHardening = &modules[i]
			break
		}
	}
	require.NotNil(t, sshHardening)

	allScripts := ""
	for _, s := range sshHardening.Scripts {
		allScripts += s.Script + " "
	}

	// REQ-004-026: All required sshd directives must be present
	requiredDirectives := []string{
		"AllowTcpForwarding local",
		"GatewayPorts no",
		"PermitTunnel no",
		"X11Forwarding no",
		"AcceptEnv SD_* ANTHROPIC_* GITHUB_* GH_*",
	}
	for _, directive := range requiredDirectives {
		assert.Contains(t, allScripts, directive,
			"ssh-hardening module must configure %q (REQ-004-026)", directive)
	}
}

func TestLoadBuiltinModules_SshHardeningDropInConfig(t *testing.T) {
	// REQ-004-026: Uses sshd_config.d drop-in for safe configuration
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	var sshHardening *Module
	for i := range modules {
		if modules[i].Name == "ssh-hardening" {
			sshHardening = &modules[i]
			break
		}
	}
	require.NotNil(t, sshHardening)

	allScripts := ""
	for _, s := range sshHardening.Scripts {
		allScripts += s.Script + " "
	}

	// Must use drop-in config directory
	assert.Contains(t, allScripts, "/etc/ssh/sshd_config.d/",
		"ssh-hardening must use sshd_config.d drop-in (REQ-004-026)")
	// Must reload (not restart) sshd to preserve sessions
	assert.Contains(t, allScripts, "reload",
		"ssh-hardening must reload sshd (not restart) to preserve sessions")
	assert.NotContains(t, allScripts, "systemctl restart",
		"ssh-hardening must NOT restart sshd (would drop sessions)")
}

func TestLoadBuiltinModules_SshHardeningSystemMode(t *testing.T) {
	// REQ-004-026: SSH config changes require root (system mode)
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	var sshHardening *Module
	for i := range modules {
		if modules[i].Name == "ssh-hardening" {
			sshHardening = &modules[i]
			break
		}
	}
	require.NotNil(t, sshHardening)
	assert.Len(t, sshHardening.Scripts, 1, "ssh-hardening should have one system-mode script")
	assert.Equal(t, ModeSystem, sshHardening.Scripts[0].Mode,
		"ssh-hardening must run as system mode (requires root)")
}

func TestLoadBuiltinModules_SshHardeningDependsOnBase(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	var sshHardening *Module
	for i := range modules {
		if modules[i].Name == "ssh-hardening" {
			sshHardening = &modules[i]
			break
		}
	}
	require.NotNil(t, sshHardening)
	assert.Contains(t, sshHardening.DependsOn, "base",
		"ssh-hardening must depend on base (REQ-006-002)")
}

func TestLoadBuiltinModules_SshHardeningHasProbe(t *testing.T) {
	// REQ-006-008: Module should have a readiness probe
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	var sshHardening *Module
	for i := range modules {
		if modules[i].Name == "ssh-hardening" {
			sshHardening = &modules[i]
			break
		}
	}
	require.NotNil(t, sshHardening)
	assert.NotNil(t, sshHardening.Probe, "ssh-hardening must have a readiness probe")
	assert.Contains(t, sshHardening.Probe.Command, "AllowTcpForwarding",
		"ssh-hardening probe must verify the sshd directive")
}

func TestLoadBuiltinModules_SshHardeningNoDownloads(t *testing.T) {
	// ssh-hardening configures sshd only, no downloads
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	var sshHardening *Module
	for i := range modules {
		if modules[i].Name == "ssh-hardening" {
			sshHardening = &modules[i]
			break
		}
	}
	require.NotNil(t, sshHardening)
	assert.False(t, sshHardening.HasDownloads(),
		"ssh-hardening should not download any files")
}

func TestLoadBuiltinModules_ScriptModes(t *testing.T) {
	// REQ-006-005: Scripts must use valid modes
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	for _, m := range modules {
		for i, s := range m.Scripts {
			assert.Contains(t, []ScriptMode{ModeSystem, ModeUser}, s.Mode,
				"module %q scripts[%d] has invalid mode %q", m.Name, i, s.Mode)
		}
	}
}

// --- REQ-004-025: DNS Filter Module Tests ---

func TestLoadBuiltinModules_DnsFilterConfiguresDnsmasq(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	var dnsFilter *Module
	for i := range modules {
		if modules[i].Name == "dns-filter" {
			dnsFilter = &modules[i]
			break
		}
	}
	require.NotNil(t, dnsFilter)

	allScripts := ""
	for _, s := range dnsFilter.Scripts {
		allScripts += s.Script + " "
	}

	assert.Contains(t, allScripts, "dnsmasq", "dns-filter must install dnsmasq")
	assert.Contains(t, allScripts, "listen-address=127.0.0.1",
		"dnsmasq must listen on 127.0.0.1 only (REQ-004-025)")
	assert.Contains(t, allScripts, "bind-interfaces",
		"dnsmasq must bind to specific interfaces (REQ-004-025)")
	assert.Contains(t, allScripts, "no-resolv",
		"dnsmasq must not use /etc/resolv.conf for upstream (REQ-004-025)")

	defaultDomains := []string{
		"api.anthropic.com", "github.com", "githubusercontent.com",
		"archive.ubuntu.com", "security.ubuntu.com", "deb.debian.org",
		"registry.npmjs.org", "pypi.org", "files.pythonhosted.org",
		"proxy.golang.org", "sum.golang.org",
	}
	for _, domain := range defaultDomains {
		assert.Contains(t, allScripts, "server=/"+domain+"/",
			"dns-filter must forward %q to upstream DNS (REQ-004-007)", domain)
	}
	assert.Contains(t, allScripts, "nameserver 127.0.0.1",
		"/etc/resolv.conf must point to local resolver (REQ-004-025)")
	assert.Contains(t, allScripts, "log-queries",
		"dnsmasq must log DNS queries for audit trail (REQ-004-025)")
}

func TestLoadBuiltinModules_DnsFilterSystemMode(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	var dnsFilter *Module
	for i := range modules {
		if modules[i].Name == "dns-filter" {
			dnsFilter = &modules[i]
			break
		}
	}
	require.NotNil(t, dnsFilter)
	assert.Len(t, dnsFilter.Scripts, 1)
	assert.Equal(t, ModeSystem, dnsFilter.Scripts[0].Mode, "dns-filter must run as system mode")
}

func TestLoadBuiltinModules_DnsFilterDependsOnBase(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	var dnsFilter *Module
	for i := range modules {
		if modules[i].Name == "dns-filter" {
			dnsFilter = &modules[i]
			break
		}
	}
	require.NotNil(t, dnsFilter)
	assert.Contains(t, dnsFilter.DependsOn, "base")
}

func TestLoadBuiltinModules_DnsFilterHasProbe(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	var dnsFilter *Module
	for i := range modules {
		if modules[i].Name == "dns-filter" {
			dnsFilter = &modules[i]
			break
		}
	}
	require.NotNil(t, dnsFilter)
	assert.NotNil(t, dnsFilter.Probe)
	assert.Contains(t, dnsFilter.Probe.Command, "127.0.0.1")
}

func TestLoadBuiltinModules_DnsFilterNoDownloads(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	var dnsFilter *Module
	for i := range modules {
		if modules[i].Name == "dns-filter" {
			dnsFilter = &modules[i]
			break
		}
	}
	require.NotNil(t, dnsFilter)
	assert.False(t, dnsFilter.HasDownloads())
}

func TestLoadBuiltinModules_DnsFilterCapturesUpstreamDns(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	var dnsFilter *Module
	for i := range modules {
		if modules[i].Name == "dns-filter" {
			dnsFilter = &modules[i]
			break
		}
	}
	require.NotNil(t, dnsFilter)
	allScripts := ""
	for _, s := range dnsFilter.Scripts {
		allScripts += s.Script + " "
	}
	assert.Contains(t, allScripts, "UPSTREAM_DNS")
	assert.Contains(t, allScripts, "/etc/resolv.conf")
}

// --- REQ-004-009: Egress Firewall Module Tests ---

func TestLoadBuiltinModules_EgressConfiguresIptables(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	var egress *Module
	for i := range modules {
		if modules[i].Name == "egress" {
			egress = &modules[i]
			break
		}
	}
	require.NotNil(t, egress)

	allScripts := ""
	for _, s := range egress.Scripts {
		allScripts += s.Script + " "
	}

	assert.Contains(t, allScripts, "iptables")
	assert.Contains(t, allScripts, "sd-egress")
	assert.Contains(t, allScripts, "iptables -N sd-egress")
	assert.Contains(t, allScripts, "-o lo -j ACCEPT",
		"must allow loopback (REQ-004-009)")
	assert.Contains(t, allScripts, "ESTABLISHED,RELATED",
		"must allow established connections (REQ-004-009)")
	assert.Contains(t, allScripts, "--dport 53 -d 127.0.0.1 -j ACCEPT",
		"must allow DNS to local resolver (REQ-004-025)")
	assert.Contains(t, allScripts, "--dport 53 -j DROP",
		"must block external DNS (REQ-004-009)")
	assert.Contains(t, allScripts, "--dport 853 -j DROP",
		"must block DNS-over-TLS (REQ-004-025)")

	dohProviders := []string{"8.8.8.8", "8.8.4.4", "1.1.1.1", "1.0.0.1", "9.9.9.9", "149.112.112.112"}
	for _, ip := range dohProviders {
		assert.Contains(t, allScripts, "-d "+ip+" -j DROP",
			"must block DoH to %s (REQ-004-025)", ip)
	}

	assert.Contains(t, allScripts, "--sport 22 -j ACCEPT",
		"must allow SSH from host (REQ-004-009)")
	assert.Contains(t, allScripts, "-A sd-egress -j DROP",
		"must have default DROP (REQ-004-006)")

	defaultDomains := []string{
		"api.anthropic.com", "github.com", "archive.ubuntu.com",
		"security.ubuntu.com", "deb.debian.org", "registry.npmjs.org",
		"pypi.org", "files.pythonhosted.org", "proxy.golang.org", "sum.golang.org",
	}
	for _, domain := range defaultDomains {
		assert.Contains(t, allScripts, domain,
			"must resolve default domain %q (REQ-004-007)", domain)
	}
}

func TestLoadBuiltinModules_EgressSystemMode(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	var egress *Module
	for i := range modules {
		if modules[i].Name == "egress" {
			egress = &modules[i]
			break
		}
	}
	require.NotNil(t, egress)
	assert.Len(t, egress.Scripts, 1)
	assert.Equal(t, ModeSystem, egress.Scripts[0].Mode, "egress must run as system mode")
}

func TestLoadBuiltinModules_EgressDependsOnDnsFilter(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	var egress *Module
	for i := range modules {
		if modules[i].Name == "egress" {
			egress = &modules[i]
			break
		}
	}
	require.NotNil(t, egress)
	assert.Contains(t, egress.DependsOn, "base")
	assert.Contains(t, egress.DependsOn, "dns-filter",
		"egress must depend on dns-filter (resolver must run before iptables blocks external DNS)")
}

func TestLoadBuiltinModules_EgressHasProbe(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	var egress *Module
	for i := range modules {
		if modules[i].Name == "egress" {
			egress = &modules[i]
			break
		}
	}
	require.NotNil(t, egress)
	assert.NotNil(t, egress.Probe)
	assert.Contains(t, egress.Probe.Command, "sd-egress")
}

func TestLoadBuiltinModules_EgressNoDownloads(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	var egress *Module
	for i := range modules {
		if modules[i].Name == "egress" {
			egress = &modules[i]
			break
		}
	}
	require.NotNil(t, egress)
	assert.False(t, egress.HasDownloads())
}

func TestLoadBuiltinModules_EgressResolvesWildcardSubdomains(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	var egress *Module
	for i := range modules {
		if modules[i].Name == "egress" {
			egress = &modules[i]
			break
		}
	}
	require.NotNil(t, egress)
	allScripts := ""
	for _, s := range egress.Scripts {
		allScripts += s.Script + " "
	}
	assert.Contains(t, allScripts, "raw.githubusercontent.com",
		"must resolve raw.githubusercontent.com for *.githubusercontent.com wildcard")
}

func TestLoadBuiltinModules_EgressPersistsRules(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	var egress *Module
	for i := range modules {
		if modules[i].Name == "egress" {
			egress = &modules[i]
			break
		}
	}
	require.NotNil(t, egress)
	allScripts := ""
	for _, s := range egress.Scripts {
		allScripts += s.Script + " "
	}
	assert.Contains(t, allScripts, "iptables-save", "must persist rules across reboots")
}

func TestLoadBuiltinModules_EgressIdempotent(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	var egress *Module
	for i := range modules {
		if modules[i].Name == "egress" {
			egress = &modules[i]
			break
		}
	}
	require.NotNil(t, egress)
	allScripts := ""
	for _, s := range egress.Scripts {
		allScripts += s.Script + " "
	}
	assert.Contains(t, allScripts, "iptables -F sd-egress", "must flush chain for idempotency")
	assert.Contains(t, allScripts, "iptables -X sd-egress", "must delete chain for idempotency")
}

func TestLoadBuiltinModules_ResolveAllSucceeds(t *testing.T) {
	// REQ-006-004: All built-in modules can be resolved together
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	resolved, err := ResolveAll(modules)
	require.NoError(t, err, "all built-in modules should resolve without circular dependencies")
	assert.Len(t, resolved, len(modules))

	// base must come first
	assert.Equal(t, "base", resolved[0].Name, "base module must be first in execution order")
}

func TestLoadBuiltinModules_ResolveSingleModule(t *testing.T) {
	// REQ-006-002: requesting any module also includes base
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	testCases := []struct{ requested, wantFirst, wantLast string }{
		{"ssh-hardening", "base", "ssh-hardening"},
		{"dns-filter", "base", "dns-filter"},
		{"egress", "base", "egress"},
		{"golang", "base", "golang"},
		{"docker", "base", "docker"},
		{"claude-code", "base", "claude-code"},
		{"rust", "base", "rust"},
		{"python", "base", "python"},
		{"github-cli", "base", "github-cli"},
	}

	for _, tc := range testCases {
		t.Run(tc.requested, func(t *testing.T) {
			resolved, err := ResolveRequested(modules, []string{tc.requested})
			require.NoError(t, err)
			require.NotEmpty(t, resolved)
			assert.Equal(t, tc.wantFirst, resolved[0].Name,
				"%s: base must come first", tc.requested)
			assert.Equal(t, tc.wantLast, resolved[len(resolved)-1].Name,
				"%s: requested module must come last", tc.requested)
		})
	}
}

// --- Property-Based Tests ---

func TestProperty_LoadBuiltinModules_AlwaysSucceeds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		modules, err := LoadBuiltinModules()
		if err != nil {
			t.Fatalf("LoadBuiltinModules should never fail: %v", err)
		}
		if len(modules) != 10 {
			t.Fatalf("expected 10 modules, got %d", len(modules))
		}
	})
}

func TestProperty_LoadBuiltinModules_AllNamesValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		modules, err := LoadBuiltinModules()
		if err != nil {
			t.Fatalf("LoadBuiltinModules failed: %v", err)
		}
		for _, m := range modules {
			if !isValidModuleName(m.Name) {
				t.Fatalf("module name %q is not valid kebab-case", m.Name)
			}
		}
	})
}

func TestProperty_LoadBuiltinModules_ResolveAnySubset(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		idx := rapid.IntRange(0, len(BuiltinModuleNames)-1).Draw(t, "idx")
		requested := []string{BuiltinModuleNames[idx]}

		modules, err := LoadBuiltinModules()
		if err != nil {
			t.Fatalf("LoadBuiltinModules failed: %v", err)
		}

		resolved, err := ResolveRequested(modules, requested)
		if err != nil {
			t.Fatalf("ResolveRequested(%v) failed: %v", requested, err)
		}

		// base is always first
		if resolved[0].Name != "base" {
			t.Fatalf("base must always be first, got %q", resolved[0].Name)
		}
	})
}

func TestProperty_LoadBuiltinModules_NoDuplicateNames(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		modules, err := LoadBuiltinModules()
		if err != nil {
			t.Fatalf("LoadBuiltinModules failed: %v", err)
		}
		seen := make(map[string]bool)
		for _, m := range modules {
			if seen[m.Name] {
				t.Fatalf("duplicate module name: %q", m.Name)
			}
			seen[m.Name] = true
		}
	})
}

func TestProperty_LoadBuiltinModules_ScriptsNotEmpty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		modules, err := LoadBuiltinModules()
		if err != nil {
			t.Fatalf("LoadBuiltinModules failed: %v", err)
		}
		for _, m := range modules {
			for i, s := range m.Scripts {
				if strings.TrimSpace(s.Script) == "" {
					t.Fatalf("module %q scripts[%d] is empty", m.Name, i)
				}
			}
		}
	})
}

// Benchmarks

func BenchmarkLoadBuiltinModules(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := LoadBuiltinModules()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolveBuiltinModules(b *testing.B) {
	modules, err := LoadBuiltinModules()
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ResolveAll(modules)
		if err != nil {
			b.Fatal(err)
		}
	}
}
