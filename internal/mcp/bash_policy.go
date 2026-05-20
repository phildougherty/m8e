package mcp

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// bashPolicyLogWriter is the sink for the construction-time mode/allowlist log
// lines emitted by LoadBashPolicy. It defaults to os.Stderr so operators see
// the message in the pod logs; tests override it via LoadBashPolicyTo so they
// can capture the output without racing against the real stderr.
var bashPolicyLogWriter io.Writer = os.Stderr

// BashPolicy governs the execute_bash tool. execute_bash runs commands through
// `bash -c`, which means it is, by construction, arbitrary code execution. The
// command string is chosen by an LLM, which may itself be steered by untrusted
// content (a file it read, a tool result, a web page). A regex blocklist of
// "dangerous" patterns is security theatre — `rm -rf /` is trivially written
// as `rm  -rf  /`, `$(echo rm) -rf /`, or a dozen other ways.
//
// So instead of pretending to filter arbitrary bash, BashPolicy takes a
// position:
//
//   - mode "allowlist" (default): every command word in the pipeline must
//     resolve to a binary on the allowlist. Pipelines, subshells, and command
//     substitutions are all walked; an un-allowed binary anywhere fails the
//     whole command. The default allowlist is read-mostly diagnostic tooling.
//   - mode "unrestricted": no command filtering. This is the honest escape
//     hatch for operators who understand the blast radius and want the agent
//     to have a real shell. It must be set explicitly.
//   - mode "disabled": execute_bash always refuses.
//
// In every mode, the child process environment is scrubbed of variables whose
// names look like secrets, so a prompt-injected `env` or `cat` cannot trivially
// exfiltrate the cluster token or provider API keys.
type BashPolicy struct {
	Mode      BashMode
	Allowlist map[string]bool
}

// BashMode is the execute_bash enforcement mode.
type BashMode string

const (
	BashModeAllowlist    BashMode = "allowlist"
	BashModeUnrestricted BashMode = "unrestricted"
	BashModeDisabled     BashMode = "disabled"
)

// defaultBashAllowlist is the set of binaries execute_bash permits when no
// operator override is supplied. It is deliberately read-mostly: tools for
// inspecting the cluster, filesystem, and processes, but not package managers,
// network fetchers piped to shells, or privilege tools. Operators who need
// more should opt into a wider allowlist or unrestricted mode explicitly.
var defaultBashAllowlist = []string{
	// cluster
	"kubectl", "matey", "helm",
	// filesystem inspection
	"ls", "cat", "head", "tail", "find", "stat", "file", "tree", "wc", "du", "df",
	// text processing
	"grep", "egrep", "fgrep", "rg", "awk", "sed", "cut", "sort", "uniq", "tr", "jq", "yq",
	// process / system inspection
	"ps", "top", "uptime", "free", "env", "printenv", "whoami", "id", "hostname", "uname", "date",
	// misc safe
	"echo", "pwd", "which", "dirname", "basename", "true", "false", "test", "diff",
	"git", "curl", "wget",
}

// LoadBashPolicy builds a BashPolicy from the process environment. It is
// intentionally env-driven for now so the fix lands without waiting on the
// larger config refactor; once MateyMCPServer takes a real config struct this
// should read from there instead (see Phase 2: service/domain layer).
//
//	MATEY_BASH_MODE      = allowlist | unrestricted | disabled  (default allowlist)
//	MATEY_BASH_ALLOWLIST = comma-separated extra binaries to permit
//
// An unrecognised mode is treated as allowlist — the safe default — and the
// caller can surface a warning.
func LoadBashPolicy() BashPolicy {

	return LoadBashPolicyTo(bashPolicyLogWriter)
}

// LoadBashPolicyTo is the test-injectable form of LoadBashPolicy. The
// production code path uses bashPolicyLogWriter (stderr); tests pass a
// *bytes.Buffer to assert the warning is emitted at construction time.
func LoadBashPolicyTo(out io.Writer) BashPolicy {
	mode := BashMode(strings.ToLower(strings.TrimSpace(os.Getenv("MATEY_BASH_MODE"))))
	switch mode {
	case BashModeUnrestricted, BashModeDisabled, BashModeAllowlist:
	default:
		mode = BashModeAllowlist
	}

	allow := make(map[string]bool, len(defaultBashAllowlist))
	for _, b := range defaultBashAllowlist {
		allow[b] = true
	}
	if extra := os.Getenv("MATEY_BASH_ALLOWLIST"); extra != "" {
		for _, b := range strings.Split(extra, ",") {
			if b = strings.TrimSpace(b); b != "" {
				allow[b] = true
			}
		}
	}

	// Always announce the resolved policy on startup so operators can verify
	// from logs which mode is active and how large the allowlist is.
	if out != nil {
		fmt.Fprintf(out, "INFO  bash_policy: mode=%s allowlist_size=%d\n", mode, len(allow))
		// And SHOUT when execute_bash is wide open. Unrestricted mode is a
		// deliberate operator choice but a foot-gun if set unintentionally,
		// so the message must be impossible to miss in pod logs.
		if mode == BashModeUnrestricted {
			fmt.Fprintln(out, "WARNING bash_policy: MATEY_BASH_MODE=unrestricted - execute_bash will run ARBITRARY shell commands from the LLM with NO allowlist filtering. This is the intended escape hatch; if you did not set this on purpose, switch to MATEY_BASH_MODE=allowlist (the safe default) or =disabled immediately.")
		}
	}

	return BashPolicy{Mode: mode, Allowlist: allow}
}

// Check returns nil if command is permitted under the policy, or an error
// explaining precisely which part of the command was rejected.
func (p BashPolicy) Check(command string) error {
	switch p.Mode {
	case BashModeDisabled:
		return fmt.Errorf("execute_bash is disabled by policy (MATEY_BASH_MODE=disabled)")
	case BashModeUnrestricted:
		return nil
	case BashModeAllowlist:
		return p.checkAllowlist(command)
	default:
		// Unreachable given LoadBashPolicy normalisation, but fail closed.
		return fmt.Errorf("execute_bash: unknown policy mode %q", p.Mode)
	}
}

// checkAllowlist tokenises the command and verifies that every word in a
// command position resolves to an allowlisted binary.
func (p BashPolicy) checkAllowlist(command string) error {
	binaries, err := commandBinaries(command)
	if err != nil {
		return fmt.Errorf("execute_bash: could not parse command for allowlist check: %w", err)
	}
	if len(binaries) == 0 {
		return fmt.Errorf("execute_bash: no executable command found")
	}
	for _, bin := range binaries {
		// Strip an absolute/relative path prefix: /usr/bin/kubectl -> kubectl.
		base := bin
		if idx := strings.LastIndexByte(base, '/'); idx >= 0 {
			base = base[idx+1:]
		}
		if !p.Allowlist[base] {
			return fmt.Errorf("execute_bash: command %q is not on the allowlist "+
				"(set MATEY_BASH_ALLOWLIST to permit it, or MATEY_BASH_MODE=unrestricted "+
				"to disable filtering entirely)", base)
		}
	}

	return nil
}

// shell metacharacters that begin a new command position.
//
// After any of these tokens, the next shell word is a binary being invoked.
// This is a pragmatic approximation of bash grammar — it intentionally errs
// toward flagging more positions as "command positions" rather than fewer, so
// the allowlist check stays conservative.
var commandSeparators = map[string]bool{
	"|": true, "||": true, "&&": true, ";": true, "&": true,
	"(": true, "{": true, "|&": true, "!": true,
}

// commandBinaries walks a shell command string and returns the binary name in
// every command position it can identify: the first word, plus the first word
// after every separator, pipe, subshell, and command substitution.
//
// It is deliberately strict: anything it cannot confidently tokenise (unbalanced
// quotes, raw backtick substitution) returns an error so the caller fails
// closed rather than waving the command through.
func commandBinaries(command string) ([]string, error) {
	tokens, err := shellTokens(command)
	if err != nil {
		return nil, err
	}

	var binaries []string
	expectCommand := true
	for _, tok := range tokens {
		if tok.isOperator {
			// A separator means the *next* real word is a command.
			if commandSeparators[tok.text] {
				expectCommand = true
			}

			continue
		}
		if expectCommand {
			// Skip leading env-assignment words like FOO=bar.
			if isEnvAssignment(tok.text) {
				continue
			}
			binaries = append(binaries, tok.text)
			expectCommand = false
		}
	}

	sort.Strings(binaries)

	return dedupe(binaries), nil
}

func isEnvAssignment(word string) bool {
	eq := strings.IndexByte(word, '=')
	if eq <= 0 {
		return false
	}
	for i := 0; i < eq; i++ {
		c := word[i]
		if c != '_' && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}

	return true
}

func dedupe(in []string) []string {
	if len(in) == 0 {
		return in
	}
	out := in[:1]
	for _, v := range in[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}

	return out
}

type shellToken struct {
	text       string
	isOperator bool
}

// shellTokens performs a minimal shell tokenisation: it splits on whitespace,
// recognises operator tokens, strips single/double quotes, and rejects command
// substitution ($(...) and backticks) because the contents would also need
// allowlist checks and a regex-free parser cannot reliably recurse into them.
// Rejecting substitution is a conservative choice: an operator who genuinely
// needs it can switch to unrestricted mode deliberately.
func shellTokens(command string) ([]shellToken, error) {
	var tokens []shellToken
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, shellToken{text: cur.String()})
			cur.Reset()
		}
	}

	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch c {
		case '`':
			return nil, fmt.Errorf("backtick command substitution is not allowed in allowlist mode")
		case '$':
			if i+1 < len(runes) && runes[i+1] == '(' {
				return nil, fmt.Errorf("$(...) command substitution is not allowed in allowlist mode")
			}
			cur.WriteRune(c)
		case '\'':
			j := i + 1
			for j < len(runes) && runes[j] != '\'' {
				cur.WriteRune(runes[j])
				j++
			}
			if j >= len(runes) {
				return nil, fmt.Errorf("unbalanced single quote")
			}
			i = j
		case '"':
			j := i + 1
			for j < len(runes) && runes[j] != '"' {
				if runes[j] == '`' || (runes[j] == '$' && j+1 < len(runes) && runes[j+1] == '(') {
					return nil, fmt.Errorf("command substitution inside double quotes is not allowed in allowlist mode")
				}
				cur.WriteRune(runes[j])
				j++
			}
			if j >= len(runes) {
				return nil, fmt.Errorf("unbalanced double quote")
			}
			i = j
		case ' ', '\t', '\n', '\r':
			flush()
		case '|', '&', ';', '(', ')', '{', '}', '<', '>':
			flush()
			// Greedily consume a two-char operator (||, &&, |&, >>, <<).
			op := string(c)
			if i+1 < len(runes) {
				two := op + string(runes[i+1])
				switch two {
				case "||", "&&", "|&", ">>", "<<":
					op = two
					i++
				}
			}
			tokens = append(tokens, shellToken{text: op, isOperator: true})
		default:
			cur.WriteRune(c)
		}
	}
	flush()

	return tokens, nil
}

// secretEnvPattern matches environment variable names that likely hold
// credentials. execute_bash scrubs these from the child process so a
// prompt-injected `env`/`printenv` cannot read them back out.
var secretEnvSubstrings = []string{
	"TOKEN", "SECRET", "PASSWORD", "PASSWD", "APIKEY", "API_KEY",
	"ACCESS_KEY", "PRIVATE_KEY", "CREDENTIAL", "SESSION", "AUTH",
}

// scrubbedEnviron returns os.Environ() with secret-looking variables removed.
// PATH, HOME, and similar operational variables are preserved so commands can
// still run.
func scrubbedEnviron() []string {
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, kv := range src {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)

			continue
		}
		name := strings.ToUpper(kv[:eq])
		secret := false
		for _, pat := range secretEnvSubstrings {
			if strings.Contains(name, pat) {
				secret = true

				break
			}
		}
		if !secret {
			out = append(out, kv)
		}
	}

	return out
}
