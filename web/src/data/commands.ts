// Built-in command vocabularies fed to the line editor's Tab completion.
// Keyed by instance type. The actual completion sources also include the
// rolling history (most recent commands first) and any per-instance custom
// list configured in the panel's edit form.

// Server-console commands take no leading slash — the slash is only for
// in-game chat input. Players who carry the slash habit should still get
// matches from history (where their own typed lines live verbatim), but
// the static vocabulary stays slash-free.
const MC_COMMON = [
  'help', 'list', 'stop', 'save-all', 'save-on', 'save-off', 'me', 'say',
  'op', 'deop', 'pardon', 'pardon-ip', 'ban', 'ban-ip', 'banlist',
  'whitelist', 'whitelist add', 'whitelist remove', 'whitelist on', 'whitelist off', 'whitelist list', 'whitelist reload',
  'kick', 'tp', 'teleport',
  'gamemode survival', 'gamemode creative', 'gamemode adventure', 'gamemode spectator',
  'gamerule', 'give', 'clear', 'effect give', 'effect clear', 'enchant',
  'time set day', 'time set night', 'time add', 'weather clear', 'weather rain', 'weather thunder',
  'difficulty peaceful', 'difficulty easy', 'difficulty normal', 'difficulty hard',
  'seed', 'setblock', 'fill', 'clone', 'locate', 'spawnpoint', 'setworldspawn',
  'xp add', 'xp set', 'scoreboard', 'team', 'title', 'tellraw',
  'reload', 'datapack list', 'datapack enable', 'datapack disable',
  'forceload add', 'forceload remove', 'forceload query',
  // Paper / Spigot extras commonly typed:
  'version', 'plugins', 'pl', 'icanhasbukkit',
  'restart', 'timings', 'mspt', 'tps',
]

const BEDROCK_COMMON = [
  'help', 'list', 'stop', 'op', 'deop', 'kick', 'whitelist', 'allowlist',
  'gamerule', 'gamemode survival', 'gamemode creative', 'gamemode adventure',
  'time set day', 'time set night', 'weather clear', 'weather rain',
  'tp', 'teleport', 'give', 'effect', 'enchant', 'difficulty',
  'reload', 'function', 'fill', 'setblock', 'summon', 'tag',
]

const TERRARIA_COMMON = [
  'help', 'exit', 'save', 'time', 'weather', 'spawnboss', 'kick', 'ban',
]

const GENERIC_COMMON = [
  'help', 'exit', 'quit', 'reload', 'status',
]

export function builtinCommands(type: string | undefined): string[] {
  switch ((type || '').toLowerCase()) {
    case 'minecraft': return MC_COMMON
    case 'bedrock':   return BEDROCK_COMMON
    case 'terraria':  return TERRARIA_COMMON
    // Docker is the catch-all in this panel — most docker instances run
    // a Java MC server, so MC vocabulary is a sensible default.
    case 'docker':    return MC_COMMON
    default:          return GENERIC_COMMON
  }
}

// splitWordList parses the textarea where the user pastes their own
// completions — one per line, comments after `#`, blanks ignored.
export function parseUserCompletions(text: string | undefined): string[] {
  if (!text) return []
  const out: string[] = []
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.replace(/#.*/, '').trim()
    if (line) out.push(line)
  }
  return out
}
