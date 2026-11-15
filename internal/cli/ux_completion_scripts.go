package cli

import (
	"context"
	"io"
)

func runUXCompletion(_ context.Context, cfg Config, args []string) error {
	if len(args) != 1 {
		return usagef("usage: kitsu completion bash|zsh|fish|powershell")
	}
	script := ""
	switch args[0] {
	case "bash":
		script = uxBashCompletion
	case "zsh":
		script = uxZshCompletion
	case "fish":
		script = uxFishCompletion
	case "powershell":
		script = uxPowerShellCompletion
	default:
		return usagef("unsupported completion shell %q; use bash, zsh, fish or powershell", args[0])
	}
	_, err := io.WriteString(cfg.Out, script)
	return err
}

const uxBashCompletion = `# bash completion for kitsu. Source this file in your shell.
_kitsu_completion() {
    local candidate
    COMPREPLY=()
    while IFS= read -r candidate; do
        COMPREPLY+=("$candidate")
    done < <(command kitsu __complete --shell bash --format lines \
        --line "${COMP_LINE:0:COMP_POINT}" \
        --word "${COMP_WORDS[COMP_CWORD]}" 2>/dev/null)
    if type compopt >/dev/null 2>&1; then
        compopt -o filenames 2>/dev/null
    fi
}
complete -o filenames -F _kitsu_completion kitsu
`

const uxZshCompletion = `#compdef kitsu
# zsh completion for kitsu. Source after compinit, or install as _kitsu.
_kitsu_completion() {
    local -a candidates
    candidates=("${(@f)$(command kitsu __complete --shell zsh --format lines -- "${(@)words[2,CURRENT]}" 2>/dev/null)}")
    (( ${#candidates} )) && compadd -- "${candidates[@]}"
}
if (( $+functions[compdef] )); then
    compdef _kitsu_completion kitsu
fi
if [[ $funcstack[1] == _kitsu ]]; then
    _kitsu_completion "$@"
fi
`

const uxFishCompletion = `# fish completion for kitsu. Save in ~/.config/fish/completions/kitsu.fish.
function __kitsu_complete
    command kitsu __complete --shell fish --format tsv --line (commandline -cp) 2>/dev/null | while read -l line
        set -l parts (string split -m 1 \t -- "$line")
        if test (count $parts) -eq 2
            printf '%s\t%s\n' (string escape -- "$parts[1]") "$parts[2]"
        end
    end
end
complete -c kitsu -f -a '(__kitsu_complete)'
`

const uxPowerShellCompletion = `# PowerShell completion for kitsu. Add this script to your profile.
Register-ArgumentCompleter -Native -CommandName kitsu -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $parts = [System.Collections.Generic.List[string]]::new()
    $hasCurrent = $false
    foreach ($element in ($commandAst.CommandElements | Select-Object -Skip 1)) {
        if ($element.Extent.StartOffset -ge $cursorPosition) { break }
        $value = $element.Extent.Text
        if ($element -is [System.Management.Automation.Language.StringConstantExpressionAst]) {
            $value = $element.Value
        }
        if ($element.Extent.EndOffset -ge $cursorPosition) {
            if ($element.Extent.EndOffset -gt $cursorPosition) { $value = $wordToComplete }
            $hasCurrent = $true
        }
        $parts.Add([string]$value)
        if ($hasCurrent) { break }
    }
    if (-not $hasCurrent) { $parts.Add('') }
    $words = $parts.ToArray()
    $json = ConvertTo-Json -InputObject $words -Compress
    $encoded = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json))
    & kitsu __complete --shell powershell --format json-base64 --words-base64 $encoded 2>$null | ForEach-Object {
        try {
            $jsonCandidate = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([string]$_))
            $item = $jsonCandidate | ConvertFrom-Json
            $text = [string]$item.value
            if ($text -match '[\s\x60$"&;|<>(){}\[\]#'']') {
                $text = "'" + $text.Replace("'", "''") + "'"
            }
            $description = [string]$item.description
            if ([string]::IsNullOrEmpty($description)) { $description = [string]$item.value }
            [System.Management.Automation.CompletionResult]::new($text, [string]$item.value, 'ParameterValue', $description)
        } catch { }
    }
}
`
