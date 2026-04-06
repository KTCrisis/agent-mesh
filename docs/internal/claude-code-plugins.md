# Claude Code Plugins — Guide pratique

> Basé sur Claude Code v2.1.92, testé le 5 avril 2026.

## Qu'est-ce qu'un plugin

Un plugin Claude Code est un package qui ajoute des capacités : MCP servers, skills (commandes), hooks (event handlers), agents, LSP servers. Il se distribue via un marketplace (officiel ou indépendant).

## Structure d'un plugin

```
mon-plugin/
├── .claude-plugin/
│   └── plugin.json          # Manifest (requis)
├── skills/                   # Skills invocables (/mon-plugin:skill-name)
│   └── ma-skill/
│       └── SKILL.md
├── agents/                   # Agents custom
│   └── mon-agent.md
├── hooks/
│   └── hooks.json            # Event handlers
├── output-styles/            # Styles de sortie custom
│   └── terse.md
├── bin/                      # Executables ajoutés au PATH
│   └── mon-outil
├── .mcp.json                 # MCP servers
├── .lsp.json                 # LSP servers
├── settings.json             # Settings par défaut
└── README.md
```

## plugin.json — Manifest

### Minimal (seul champ requis)

```json
{
  "name": "mon-plugin"
}
```

### Complet

```json
{
  "name": "mon-plugin",
  "version": "1.0.0",
  "description": "Ce que fait le plugin",
  "author": {
    "name": "Nom",
    "email": "email@example.com",
    "url": "https://github.com/user"
  },
  "homepage": "https://docs.example.com",
  "repository": "https://github.com/user/mon-plugin",
  "license": "MIT",
  "keywords": ["mot-cle1", "mot-cle2"],
  "userConfig": {
    "api_key": {
      "description": "Clé API du service",
      "sensitive": true
    },
    "endpoint": {
      "description": "URL du endpoint",
      "sensitive": false
    }
  }
}
```

### Champs

| Champ | Type | Requis | Description |
|-------|------|--------|-------------|
| `name` | string | oui | Identifiant kebab-case, utilisé comme namespace (`/name:skill`) |
| `version` | string | non | Semver (`1.2.3`) |
| `description` | string | non | Description courte |
| `author` | object | non | `{name, email?, url?}` |
| `homepage` | string | non | URL de la doc |
| `repository` | string | non | URL du code source |
| `license` | string | non | SPDX (`MIT`, `Apache-2.0`) |
| `keywords` | array | non | Tags pour la découverte |
| `userConfig` | object | non | Config demandée à l'install |

> **Note v2.1.92** : certains champs avancés (`author` comme objet, `userConfig`) peuvent causer des erreurs de validation du manifest. Tester avec un manifest minimal d'abord.

## Skills

### Structure

```
skills/
└─�� ma-skill/
    ├── SKILL.md          # Requis — instructions + frontmatter
    ���── reference.md      # Optionnel — contexte additionnel
    └── scripts/
        └── helper.sh     # Optionnel — scripts utilitaires
```

### SKILL.md — Format

```yaml
---
name: ma-skill
description: Quand utiliser cette skill (max 250 chars)
user-invocable: true
argument-hint: "[fichier] [format]"
allowed-tools:
  - Read
  - Write
  - Bash
model: sonnet
effort: medium
context: fork
---

Instructions pour Claude quand la skill est invoquée.

Utiliser $ARGUMENTS pour capturer les arguments.
$0 = premier argument, $1 = deuxième, etc.
${CLAUDE_SKILL_DIR} = dossier contenant SKILL.md
${CLAUDE_SESSION_ID} = ID de la session courante
```

### Champs frontmatter

| Champ | Type | Défaut | Description |
|-------|------|--------|-------------|
| `name` | string | nom du dossier | Identifiant (lowercase, tirets) |
| `description` | string | premier paragraphe | Quand déclencher la skill |
| `user-invocable` | boolean | true | Si false, seul Claude peut invoquer |
| `disable-model-invocation` | boolean | false | Si true, seul l'user peut invoquer |
| `argument-hint` | string | — | Hint affiché dans l'UI |
| `allowed-tools` | array | — | Tools autorisés sans demander permission |
| `model` | string | modèle session | Override le modèle (`sonnet`, `opus`, `haiku`) |
| `effort` | string | effort session | `low`, `medium`, `high`, `max` |
| `context` | string | — | `fork` pour exécuter en sous-agent isolé |
| `agent` | string | general-purpose | Type de sous-agent si `context: fork` |
| `paths` | string/array | — | Globs limitant l'activation |
| `shell` | string | bash | `bash` ou `powershell` |

## MCP Servers — .mcp.json

```json
{
  "mcpServers": {
    "mon-server": {
      "command": "node",
      "args": ["${CLAUDE_PLUGIN_ROOT}/server.js"],
      "env": {
        "API_KEY": "${user_config.api_key}"
      }
    }
  }
}
```

### Variables disponibles

| Variable | Description |
|----------|-------------|
| `${CLAUDE_PLUGIN_ROOT}` | Dossier d'installation du plugin (change à chaque update) |
| `${CLAUDE_PLUGIN_DATA}` | Dossier persistant (`~/.claude/plugins/data/{id}/`) |
| `${user_config.KEY}` | Valeurs de `userConfig` renseignées par l'utilisateur |

## Hooks — hooks.json

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "echo 'avant chaque Bash'",
            "timeout": 5000
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "command",
            "command": "npm run lint:fix"
          }
        ]
      }
    ]
  }
}
```

### Events disponibles

| Event | Description |
|-------|-------------|
| `SessionStart` / `SessionEnd` | Début/fin de session |
| `PreToolUse` / `PostToolUse` | Avant/après un tool call |
| `PostToolUseFailure` | Après un tool call échoué |
| `UserPromptSubmit` | Soumission d'un prompt |
| `SubagentStart` / `SubagentStop` | Lancement/arrêt de sous-agent |
| `Stop` | Arrêt de Claude |

### Types de hooks

| Type | Description |
|------|-------------|
| `command` | Exécute une commande shell |
| `http` | POST le JSON de l'event à une URL |
| `prompt` | Évalue avec un LLM |
| `agent` | Lance un agent vérificateur |

## Distribution — Marketplace

### marketplace.json

```json
{
  "name": "mon-marketplace",
  "owner": {
    "name": "Mon Nom",
    "email": "email@example.com"
  },
  "metadata": {
    "description": "Description du marketplace",
    "version": "1.0.0"
  },
  "plugins": [
    {
      "name": "mon-plugin",
      "source": "./",
      "description": "Ce que fait le plugin",
      "version": "1.0.0",
      "license": "MIT",
      "category": "infrastructure"
    }
  ]
}
```

Placé dans `.claude-plugin/marketplace.json`.

### Sources de plugins supportées

```json
// Chemin relatif (même repo)
"source": "./plugins/mon-plugin"

// GitHub
"source": { "source": "github", "repo": "user/repo", "ref": "v1.0.0" }

// Git URL
"source": { "source": "url", "url": "https://gitlab.com/team/plugin.git" }

// Sous-dossier d'un monorepo
"source": { "source": "git-subdir", "url": "https://github.com/org/mono.git", "path": "tools/plugin" }

// npm
"source": { "source": "npm", "package": "@org/plugin", "version": "^1.0.0" }
```

### Noms réservés (interdits)

`claude-code-marketplace`, `claude-plugins-official`, `anthropic-plugins`, `anthropic-marketplace`, `agent-skills`

## Commandes utilisateur

```bash
# Marketplaces
/plugin marketplace add user/repo          # Ajouter depuis GitHub
/plugin marketplace add ./local-path       # Ajouter depuis un dossier local
/plugin marketplace list                   # Lister les marketplaces
/plugin marketplace update nom             # Mettre à jour
/plugin marketplace remove nom             # Supprimer

# Plugins
/plugin install nom@marketplace            # Installer un plugin
/plugin uninstall nom                      # Désinstaller
/plugin list                               # Lister les plugins installés

# Gestion
/reload-plugins                            # Recharger tous les plugins
/doctor                                    # Diagnostics (erreurs de plugins)

# Test local
claude --plugin-dir ./mon-plugin           # Charger un plugin pour une session
```

### Scopes d'installation

| Scope | Description |
|-------|-------------|
| **user** | Pour toi, tous les projets |
| **project** | Pour tous les collaborateurs du repo |
| **local** | Pour toi, dans ce repo uniquement |

## Gotchas (v2.1.92)

1. **Le compteur de skills dans `/reload-plugins` affiche 0** même quand les skills fonctionnent. Bug d'affichage confirmé.

2. **`--plugin-dir` charge les MCP servers mais pas les skills**. Pour tester les skills, installer le plugin via `/plugin install`.

3. **Le manifest minimal est le plus fiable**. Commencer avec `name` + `description` + `version` uniquement, ajouter les champs un par un.

4. **`userConfig` dans plugin.json peut casser la validation**. Si le manifest ne charge pas, retirer ce champ d'abord.

5. **Les skills du plugin sont namespaced** : `/nom-plugin:nom-skill`. Les skills dans `.claude/skills/` du projet sont invoquées directement : `/nom-skill`.
