#!/usr/bin/env bash
# Install LJM Grok hooks, skills, agents, and global rules (Linux/macOS).
# Usage: ./grok/install.sh [--vault-root <path>] [--uninstall]
set -euo pipefail

VaultRoot=""
Uninstall=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --vault-root)
      VaultRoot="${2:?--vault-root requires a path}"
      shift 2
      ;;
    --uninstall)
      Uninstall=1
      shift
      ;;
    -h|--help)
      echo "Usage: $0 [--vault-root <path>] [--uninstall]"
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 2
      ;;
  esac
done

ScriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -z "$VaultRoot" ]]; then
  VaultRoot="$(cd "$ScriptDir/.." && pwd)"
fi
VaultRoot="$(cd "$VaultRoot" && pwd)"

GrokHome="${HOME}/.grok"
HookDest="${GrokHome}/hooks/ljm.json"
SkillsDest="${GrokHome}/skills"
AgentsDest="${GrokHome}/agents"
GlobalGrok="${GrokHome}/GROK.md"
ConfigSnippet="${GrokHome}/ljm-config.snippet.toml"
HookRunner="${VaultRoot}/grok/bin/run-hook"
HookTemplate="${VaultRoot}/grok/hooks/ljm.template.json"

install_tree() {
  local source="$1" dest="$2"
  mkdir -p "$dest"
  shopt -s nullglob
  for dir in "$source"/*/; do
    local name
    name="$(basename "$dir")"
    rm -rf "${dest}/${name}"
    cp -a "$dir" "${dest}/${name}"
  done
  shopt -u nullglob
}

render_hooks() {
  local template="$1" dest="$2" runner="$3" vault="$4"
  if [[ ! -f "$template" ]]; then
    echo "Hook template missing: $template" >&2
    exit 1
  fi
  # Escape sed replacement metacharacters (&, \, |) in paths.
  local esc_runner esc_vault
  esc_runner=$(printf '%s\n' "$runner" | sed -e 's/[&\\|]/\\&/g')
  esc_vault=$(printf '%s\n' "$vault" | sed -e 's/[&\\|]/\\&/g')
  sed \
    -e "s|__HOOK_RUNNER__|${esc_runner}|g" \
    -e "s|__JM_VAULT_ROOT__|${esc_vault}|g" \
    "$template" >"$dest"
}

if [[ "$Uninstall" -eq 1 ]]; then
  rm -f "$HookDest" "$GlobalGrok"
  for s in memory-associator memory-daydream memory-associate memory-buffer memory-consolidate; do
    rm -rf "${SkillsDest}/${s}"
  done
  for a in memory-associator memory-daydream; do
    rm -f "${AgentsDest}/${a}.md"
  done
  echo "LJM Grok integration removed from $GrokHome"
  exit 0
fi

# Build agent/jm if missing (native Linux/macOS binary).
jm="${VaultRoot}/agent/jm"
if [[ ! -x "$jm" ]]; then
  echo "Building agent/jm..."
  (cd "${VaultRoot}/agent" && go build -o jm .)
fi

# Optional convenience symlink at vault root (mirrors Windows jm.exe layout).
if [[ ! -e "${VaultRoot}/jm" ]]; then
  ln -sfn agent/jm "${VaultRoot}/jm" 2>/dev/null || true
fi

chmod +x "$HookRunner" "${VaultRoot}/grok/bin/run-hook.sh"

# Hooks — expand shared template with this platform's runner (global only).
mkdir -p "${GrokHome}/hooks"
render_hooks "$HookTemplate" "$HookDest" "$HookRunner" "$VaultRoot"

# Skills and agents
install_tree "${VaultRoot}/.grok/skills" "$SkillsDest"
mkdir -p "$AgentsDest"
shopt -s nullglob
cp -a "${VaultRoot}/.grok/agents/"*.md "$AgentsDest/"
shopt -u nullglob

# Global memory protocol — rewrite vault location for this host
cp -a "${VaultRoot}/grok/global/GROK.md" "$GlobalGrok"
# Replace common Windows default path tokens with this install path
sed -i \
  -e "s|D:\\\\Repos\\\\LLM\\\\LittleJohnnyMnemonic\\\\|${VaultRoot}/|g" \
  -e "s|D:/Repos/LLM/LittleJohnnyMnemonic|${VaultRoot}|g" \
  -e "s|D:\\\\Repos\\\\LLM\\\\LittleJohnnyMnemonic|${VaultRoot}|g" \
  -e "s|D:/repos/llm/littlejohnnymnemonic|${VaultRoot}|g" \
  -e "s|D:\\\\repos\\\\llm\\\\littlejohnnymnemonic|${VaultRoot}|g" \
  -e "s|\\\\.\\\\grok\\\\install\\.ps1|./grok/install.sh|g" \
  -e "s|\\\\.\\\\grok\\\\install\\.ps1|./grok/install.sh|g" \
  -e "s|\`\\\\.\\\\grok\\\\install\\.ps1\`|\`./grok/install.sh\`|g" \
  -e "s|install\\.ps1|install.sh|g" \
  -e "s|jm\\.exe|jm|g" \
  "$GlobalGrok"

# Optional config snippet
if [[ -f "${VaultRoot}/grok/config.toml.example" ]]; then
  cp -a "${VaultRoot}/grok/config.toml.example" "$ConfigSnippet"
fi

echo "Installed LJM Grok integration:"
echo "  Vault:   $VaultRoot"
echo "  Hooks:   $HookDest"
echo "  Runner:  $HookRunner"
echo "  Skills:  $SkillsDest/memory-*"
echo "  Agents:  $AgentsDest/memory-*.md"
echo "  Rules:   $GlobalGrok"
echo "  Binary:  $jm"
if [[ -f "$ConfigSnippet" ]]; then
  echo "  Config:  $ConfigSnippet (merge into ~/.grok/config.toml)"
fi
echo ""
echo "Verify agents: /config-agents — memory-daydream and memory-associator should appear."
echo "Restart Grok sessions (or press r in /hooks) to load hooks."
echo "Smoke test:"
echo "  echo '{\"sessionId\":\"test\",\"workspaceRoot\":\"${VaultRoot}\"}' | JM_VAULT_ROOT=${VaultRoot} ${HookRunner} session-start | head"
