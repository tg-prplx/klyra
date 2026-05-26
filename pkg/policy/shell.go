package policy

import (
	"strings"
)

type Risk string
type Sandbox string

const (
	RiskRead        Risk = "read"
	RiskWrite       Risk = "write"
	RiskNetwork     Risk = "network"
	RiskDestructive Risk = "destructive"

	SandboxReadOnly         Sandbox = "read-only"
	SandboxWorkspaceWrite   Sandbox = "workspace-write"
	SandboxDangerFullAccess Sandbox = "danger-full-access"
)

type Assessment struct {
	Command          string `json:"command"`
	Risk             Risk   `json:"risk"`
	Reason           string `json:"reason"`
	RequiresApproval bool   `json:"requires_approval"`
	BlockInAuto      bool   `json:"block_in_auto"`
}

func AssessShellCommand(command string) Assessment {
	normalized := strings.ToLower(strings.TrimSpace(command))
	assessment := Assessment{
		Command: command,
		Risk:    RiskRead,
		Reason:  "read-only or low-impact command",
	}
	if normalized == "" {
		assessment.Reason = "empty command"
		assessment.BlockInAuto = true
		return assessment
	}
	if containsAny(normalized, destructivePatterns()) {
		assessment.Risk = RiskDestructive
		assessment.Reason = "matches destructive shell pattern"
		assessment.RequiresApproval = true
		assessment.BlockInAuto = true
		return assessment
	}
	if containsAny(normalized, networkPatterns()) {
		assessment.Risk = RiskNetwork
		assessment.Reason = "may access network or external hosts"
		assessment.RequiresApproval = true
		return assessment
	}
	if containsAny(normalized, writePatterns()) {
		assessment.Risk = RiskWrite
		assessment.Reason = "may modify workspace or local state"
		assessment.RequiresApproval = true
		return assessment
	}
	return assessment
}

func NormalizeSandbox(value string) Sandbox {
	switch Sandbox(strings.ToLower(strings.TrimSpace(value))) {
	case SandboxReadOnly:
		return SandboxReadOnly
	case SandboxDangerFullAccess:
		return SandboxDangerFullAccess
	default:
		return SandboxWorkspaceWrite
	}
}

func IsAllowedInSandbox(assessment Assessment, sandbox Sandbox) (bool, string) {
	switch NormalizeSandbox(string(sandbox)) {
	case SandboxDangerFullAccess:
		return true, "allowed by danger-full-access sandbox"
	case SandboxReadOnly:
		if assessment.Risk == RiskRead {
			return true, "allowed read-only command"
		}
		return false, "read-only sandbox blocks non-read command"
	default:
		if assessment.Risk == RiskDestructive || assessment.Risk == RiskNetwork {
			return false, "workspace-write sandbox blocks network/destructive command"
		}
		return true, "allowed by workspace-write sandbox"
	}
}

func destructivePatterns() []string {
	return []string{
		"rm -rf /",
		"rm -fr /",
		"git reset --hard",
		"git clean -fd",
		"mkfs",
		"dd if=",
		"dd of=",
		"chmod -r 777",
		"chown -r",
		"sudo ",
		"shutdown",
		"reboot",
		"kill -9",
		":(){",
		"| sh",
		"| bash",
	}
}

func networkPatterns() []string {
	return []string{
		"git clone",
		"git fetch",
		"git pull",
		"git push",
		"ssh ",
		"scp ",
		"rsync ",
		"nc ",
		"ncat ",
		"telnet ",
		"curl ",
		"wget ",
		"npm install",
		"pnpm install",
		"yarn add",
		"go get ",
		"pip install",
		"cargo install",
	}
}

func writePatterns() []string {
	return []string{
		">",
		">>",
		" tee ",
		"touch ",
		"mkdir ",
		"mv ",
		"cp ",
		"rm ",
		"sed -i",
		"perl -pi",
		"git add",
		"git commit",
		"git apply",
		"go mod tidy",
		"npm run build",
		"cargo build",
	}
}

func containsAny(text string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}
