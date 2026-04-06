# Comprendre la structure Go d'Agent Mesh

Ce document explique comment le code Go est organisé, pour quelqu'un qui ne connaît pas le langage.

## Les 3 opérations d'Agent Mesh

Avant de plonger dans le code, il faut comprendre les 3 opérations distinctes :

| Opération | Direction | Ce que ça fait | Code |
|-----------|-----------|---------------|------|
| **Import OpenAPI** | Swagger → registry | Parse une spec OpenAPI, enregistre les endpoints comme tools | `registry/openapi.go` |
| **Import MCP** | MCP servers → registry | Se connecte à des MCP servers, découvre et enregistre leurs tools | `mcp/client.go` + `registry/mcp.go` |
| **Export MCP** | registry → MCP client | Expose tous les tools (peu importe leur source) comme un MCP server | `mcp/server.go` |

```
Import OpenAPI ──▶ ┌──────────┐ ──▶ HTTP proxy (agents HTTP)
                   │ Registry  │
Import MCP     ──▶ │ (tools)   │ ──▶ Export MCP (Claude, Cursor)
                   └─────┬─────┘
                   policy · trace
```

Ces 3 opérations sont **combinables** : on peut importer depuis un Swagger ET des MCP servers, puis re-exporter le tout comme MCP pour Claude.

## Concepts Go de base

### Package = dossier

En Go, **un dossier = un package**. Tous les fichiers `.go` dans un même dossier partagent le même `package` et peuvent accéder aux fonctions/types des autres fichiers du même dossier sans import.

```
registry/
├── registry.go    ← package registry
├── openapi.go     ← package registry (même package, voit tout)
├── mcp.go         ← package registry (même package, voit tout)
└── registry_test.go  ← package registry (tests)
```

### Public vs privé = majuscule vs minuscule

Go n'a pas de `public`/`private`. La convention est simple :
- **Majuscule** = exporté (visible de l'extérieur) → `Tool`, `NewManager`, `LoadMCP`
- **Minuscule** = interne au package → `rpcRequest`, `setStatus`, `toInt64`

```go
type Tool struct { ... }      // Exporté — les autres packages peuvent l'utiliser
type rpcRequest struct { ... } // Interne — seulement visible dans le package mcp
```

### Struct = objet (sans classe)

Go n'a pas de classes. On utilise des **structs** (structures de données) avec des **méthodes** attachées :

```go
// La struct (comme une classe sans héritage)
type Registry struct {
    mu    sync.RWMutex       // champ privé (minuscule)
    tools map[string]*Tool   // champ privé
}

// Une méthode sur Registry (le "r" est l'équivalent de "this" ou "self")
func (r *Registry) Get(name string) *Tool {
    return r.tools[name]
}
```

### Interface = contrat

Une interface définit un contrat : "si tu as ces méthodes, tu implémentes l'interface". Pas besoin de `implements` explicite — c'est automatique.

```go
// L'interface (définie dans proxy/)
type MCPForwarder interface {
    CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (any, error)
    ServerStatuses() any
}

// Manager implémente MCPForwarder automatiquement
// parce qu'il a les bonnes méthodes avec les bonnes signatures
type Manager struct { ... }
func (m *Manager) CallTool(...) (any, error) { ... }  // ✓ match
func (m *Manager) ServerStatuses() any { ... }          // ✓ match
```

On utilise ça pour **casser les cycles d'import** : `proxy` ne connaît pas le package `mcp`, il connaît juste l'interface.

### Goroutine = thread léger

`go maFonction()` lance une goroutine (thread ultra-léger). On en utilise pour :
- Le `readLoop()` du client MCP (lit les réponses en continu)
- Le logger stderr du subprocess
- Le signal handler pour le graceful shutdown

### Channel = canal de communication

Les channels permettent aux goroutines de communiquer de façon sûre :

```go
ch := make(chan rpcResponse, 1)  // canal qui transporte des rpcResponse
ch <- resp                        // envoyer une réponse
resp := <-ch                      // attendre une réponse
```

### Mutex = verrou

Protège les données partagées entre goroutines :

```go
sync.Mutex      // un seul accès à la fois (lecture ou écriture)
sync.RWMutex    // plusieurs lecteurs OU un seul écrivain
```

---

## Structure du projet

```
agent-mesh/
├── main.go                    # Point d'entrée
├── config/
│   ├── config.go              # Types de config + chargement YAML
│   └── config_test.go
├── registry/
│   ├── registry.go            # Types partagés + Registry CRUD
│   ├── openapi.go             # Import OpenAPI (Swagger → tools)
│   ├── mcp.go                 # Import MCP (MCP tools → registry)
│   └── registry_test.go
├── policy/
│   ├── engine.go              # Moteur d'évaluation des règles
│   └── engine_test.go
├── proxy/
│   ├── handler.go             # Handler HTTP + interface MCPForwarder
│   └── handler_test.go
├── mcp/
│   ├── server.go              # Export MCP (expose tools via stdio JSON-RPC)
│   ├── client.go              # Import MCP (connexion aux MCP servers upstream)
│   ├── manager.go             # Gère N connexions Import MCP
│   ├── client_test.go
│   └── manager_test.go
├── trace/
│   ├── store.go               # Store de traces en mémoire
│   └── store_test.go
├── config.yaml              # Config exemple
├── go.mod                     # Dépendances (comme package.json)
└── go.sum                     # Lock file (comme package-lock.json)
```

---

## Fichier par fichier

### `main.go` — Point d'entrée

C'est le chef d'orchestre. Il ne contient aucune logique métier, juste le câblage :

```go
func main() {
    // 1. Parse les flags CLI (--config, --openapi, --mcp, etc.)
    // 2. Charge la config YAML
    // 3. Crée le Registry (catalogue de tools)
    // 4. Charge les tools OpenAPI si --openapi fourni
    // 5. Crée le Policy Engine
    // 6. Crée le Trace Store
    // 7. Crée le Handler HTTP
    // 8. Connecte les MCP servers upstream si configurés
    // 9. Lance en mode MCP (stdio) ou HTTP (serveur web)
}
```

Il contient aussi `convertMCPTools()` — une fonction pont entre les types du package `mcp` et ceux du package `registry`.

---

### `config/config.go` — Configuration

Définit la structure de la config YAML et la charge depuis un fichier.

**Types :**

```
Config
├── Port           int                 # port HTTP (défaut: 9090)
├── MCPServers     []MCPServerConfig   # serveurs MCP upstream
│   ├── Name       string              # nom unique ("filesystem")
│   ├── Transport  string              # "stdio" ou "sse"
│   ├── Command    string              # binaire à lancer (stdio)
│   ├── Args       []string            # arguments
│   ├── Env        map[string]string   # variables d'environnement
│   ├── URL        string              # URL (sse)
│   └── Headers    map[string]string   # headers HTTP (sse)
└── Policies       []Policy            # règles de gouvernance
    ├── Name       string              # nom de la policy
    ├── Agent      string              # pattern agent ("support-*")
    └── Rules      []Rule
        ├── Tools      []string        # tools concernés
        ├── Action     string          # "allow", "deny", "human_approval"
        └── Condition  *Condition      # optionnel
            ├── Field    string        # "params.amount"
            ├── Operator string        # "<", ">=", "==", etc.
            └── Value    float64       # valeur de comparaison
```

**Fonctions :**
- `Load(path) → (*Config, error)` — lit le YAML, parse, applique les défauts

---

### `registry/registry.go` — Types et Registry

Le registre central de tous les tools. Thread-safe grâce à un `RWMutex`.

**Types :**

```
Tool
├── Name        string              # identifiant unique ("get_pet" ou "filesystem.read_file")
├── Description string              # description humaine
├── Method      string              # "GET", "POST"... (vide pour MCP)
├── Path        string              # "/pet/{petId}" (vide pour MCP)
├── BaseURL     string              # "https://petstore.swagger.io" (vide pour MCP)
├── Params      []Param             # paramètres du tool
├── Headers     map[string]string   # headers backend (non exposés aux agents)
├── Source      string              # "openapi" ou "mcp"
└── MCPServer   string              # nom du serveur MCP source (si MCP)

Param
├── Name     string    # "petId", "path", etc.
├── In       string    # "path", "query", "body"
├── Type     string    # "string", "integer", "boolean"
└── Required bool
```

**Méthodes du Registry :**
- `New()` → crée un registry vide
- `Get(name)` → récupère un tool par nom
- `All()` → liste tous les tools
- `Remove(name)` → supprime un tool
- `LoadManual(tool)` → enregistre un tool manuellement

---

### `registry/openapi.go` — Import OpenAPI

Télécharge une spec OpenAPI (Swagger), la parse, et enregistre chaque endpoint comme un `Tool`. C'est l'opération **Import OpenAPI**.

```
GET /pet/{petId}  →  Tool{ Name: "get_pet_by_id", Method: "GET", Path: "/pet/{petId}" }
POST /pet         →  Tool{ Name: "add_pet", Method: "POST", Path: "/pet" }
```

**Fonctions :**
- `LoadOpenAPI(specURL, backendURL, headers)` → charge une spec et enregistre les tools
- `buildToolName(method, path, op)` → génère un nom snake_case depuis l'operationId ou le path
- `extractParams(op)` → extrait les paramètres de l'opération
- `inferBaseURL(spec)` → déduit l'URL du backend depuis la spec

---

### `registry/mcp.go` — Import MCP (côté registry)

Enregistre les tools découverts depuis un serveur MCP upstream dans le registry. C'est la partie "enregistrement" de l'opération **Import MCP** (la connexion est dans `mcp/client.go`).

**Types :**

```
MCPToolDef                    # format d'entrée (ce que main.go envoie)
├── Name        string
├── Description string
└── Params      []Param

MCPPropDef                    # propriété simplifiée pour la conversion
└── Type        string
```

**Fonctions :**
- `LoadMCP(serverName, tools)` → enregistre les tools avec namespace (`filesystem.read_file`)
- `RemoveByServer(serverName)` → supprime tous les tools d'un serveur (utile pour reconnexion)
- `NewMCPToolDef(name, desc, props, required)` → crée un MCPToolDef depuis des données brutes

---

### `policy/engine.go` — Moteur de policies

Évalue si un agent a le droit d'appeler un tool avec des paramètres donnés.

**Types :**

```
Engine
└── policies []config.Policy    # les règles chargées depuis le YAML

Decision                        # résultat d'une évaluation
├── Action  string              # "allow", "deny", "human_approval"
├── Rule    string              # nom de la policy qui a matché
└── Reason  string              # explication lisible
```

**Logique d'évaluation :**
```
Pour chaque policy (dans l'ordre du YAML) :
  L'agent matche le pattern ? (support-* matche support-bot)
    Pour chaque rule :
      Le tool est dans la liste ? (* matche tout)
        La condition est remplie ? (params.amount < 500)
          → Retourne la décision (allow/deny/human_approval)

Rien n'a matché → deny (fail closed)
```

**Fonctions internes :**
- `matchAgent(pattern, agentID)` → glob matching (`support-*` matche `support-bot`)
- `matchTool(tools, toolName)` → cherche le tool dans la liste (`*` = tout)
- `evaluateCondition(cond, params)` → évalue `params.amount < 500`
- `extractField(field, data)` → navigue un chemin pointé (`params.amount` → `data["params"]["amount"]`)

---

### `proxy/handler.go` — Handler HTTP

Le coeur du proxy. Reçoit les requêtes HTTP des agents, applique le pipeline, et répond.

**Types :**

```
MCPForwarder (interface)          # contrat pour le forwarding MCP
├── CallTool(ctx, server, tool, args) → (result, error)
└── ServerStatuses() → any

Handler
├── Registry      *Registry       # catalogue de tools
├── Policy        *Engine         # moteur de rules
├── Traces        *Store          # store de traces
├── Client        *http.Client    # client HTTP pour les backends REST
└── MCPForwarder  MCPForwarder    # forwarder MCP (nil si pas de MCP upstream)

ToolCallRequest                   # corps JSON envoyé par l'agent
└── Params  map[string]any        # { "petId": 1 }

ToolCallResponse                  # réponse renvoyée à l'agent
├── Result     any                # résultat du backend
├── TraceID    string             # ID de trace
├── Policy     string             # décision policy
├── LatencyMs  int64              # latence en ms
└── Error      string             # erreur éventuelle
```

**Routes HTTP :**
```
POST /tool/{name}    → handleToolCall     # pipeline complet
GET  /tools          → handleListTools    # liste les tools
GET  /mcp-servers    → handleMCPServers   # liste les MCP servers
GET  /traces         → handleTraces       # historique des traces
GET  /health         → handleHealth       # santé + stats
```

**Pipeline d'un tool call :**
```
handleToolCall
  1. Parse le JSON body
  2. Cherche le tool dans le registry
  3. Évalue la policy (allow/deny/human_approval)
  4. Forward() → forwardHTTP() ou forwardMCP() selon tool.Source
  5. Enregistre la trace
  6. Retourne la réponse
```

---

### `mcp/server.go` — Export MCP

C'est l'opération **Export MCP** : agent-mesh expose tous les tools du registry comme un serveur MCP. Communique via stdin/stdout en JSON-RPC. C'est ce qui permet à Claude ou Cursor de consommer des APIs REST sans les connaître.

**Types partagés (utilisés aussi par client.go) :**

```
rpcRequest (interne)              # requête JSON-RPC 2.0
├── JSONRPC  string               # toujours "2.0"
├── ID       any                  # identifiant de requête
├── Method   string               # "initialize", "tools/list", "tools/call"
└── Params   map[string]any       # paramètres

rpcResponse (interne)
├── JSONRPC  string
├── ID       any
├── Result   any                  # résultat si succès
└── Error    *rpcError            # erreur si échec

MCPTool (exporté)                 # format MCP d'un tool
├── Name        string
├── Description string
└── InputSchema MCPSchema
    ├── Type       string         # "object"
    ├── Properties map[string]MCPProp
    └── Required   []string

Server
├── Registry  *Registry
├── Policy    *Engine
├── Traces    *Store
├── Handler   *Handler            # pour le forwarding
└── AgentID   string              # identité de l'agent en mode MCP
```

**Méthodes MCP gérées :**
```
initialize            → retourne les capabilities du serveur
notifications/initialized → ack client (pas de réponse)
tools/list            → retourne tous les tools au format MCP
tools/call            → exécute un tool (même pipeline policy/trace)
ping                  → pong
```

---

### `mcp/client.go` — Import MCP (côté connexion)

C'est la partie "connexion" de l'opération **Import MCP** : agent-mesh se connecte à des serveurs MCP en amont via stdio, fait le handshake, et découvre leurs tools.

**Type :**

```
MCPClient
├── Name       string              # nom du serveur ("filesystem")
├── Transport  string              # "stdio"
├── cmd        *exec.Cmd           # processus fils
├── stdin      io.WriteCloser      # écriture vers le subprocess
├── stdout     *bufio.Reader       # lecture depuis le subprocess
├── writeMu    sync.Mutex          # protège les écritures stdin
├── stateMu    sync.Mutex          # protège tools/status
├── nextID     atomic.Int64        # compteur de request IDs
├── pending    map[int64]chan       # requêtes en attente de réponse
├── pendingMu  sync.Mutex          # protège pending
├── tools      []MCPTool           # tools découverts
├── status     string              # "connecting", "ready", "error", "closed"
├── lastError  string
└── done       chan struct{}        # fermé quand readLoop s'arrête
```

**Cycle de vie :**
```
NewStdioClient(name, command, args, env)
  → crée le MCPClient, prépare la commande

Connect(ctx)
  → lance le subprocess
  → démarre readLoop() en goroutine
  → envoie "initialize" et attend la réponse
  → envoie "notifications/initialized"
  → envoie "tools/list" et parse les tools
  → status = "ready"
  → si erreur à n'importe quelle étape → Close() + return error

CallTool(ctx, name, arguments)
  → envoie "tools/call" via send()
  → attend la réponse ou timeout

Close()
  → status = "closed"
  → drain toutes les requêtes pending
  → ferme stdin
  → kill le subprocess avec timeout 5s
```

**Multiplexage des requêtes :**
```
send() crée un channel, l'enregistre dans pending[id], envoie la requête
readLoop() lit les réponses, matche par id, envoie sur le bon channel
send() reçoit la réponse via le channel

Si readLoop() meurt → close(done) → tous les send() en attente sont débloqués
```

---

### `mcp/manager.go` — Manager MCP

Gère N connexions MCP upstream. Implémente l'interface `MCPForwarder` du package `proxy`.

**Types :**

```
ServerStatus                      # statut d'un serveur (retourné par GET /mcp-servers)
├── Name       string
├── Transport  string
├── Status     string             # "ready", "error", "closed"
├── Error      string             # message d'erreur si applicable
└── Tools      []string           # noms des tools

Manager
├── mu       sync.RWMutex
└── clients  map[string]*MCPClient
```

**Méthodes :**
- `Add(client)` → enregistre un client
- `Get(name)` → récupère un client par nom
- `All()` → liste tous les clients
- `ServerStatuses()` → retourne le statut de chaque serveur (pour l'API)
- `CallTool(ctx, serverName, toolName, args)` → forward vers le bon client
- `CloseAll()` → ferme toutes les connexions

---

### `trace/store.go` — Store de traces

Stocke l'historique de tous les tool calls en mémoire, avec eviction circulaire.

**Types :**

```
Entry
├── TraceID     string             # ID unique auto-généré
├── AgentID     string             # qui a appelé
├── Tool        string             # quel tool
├── Params      map[string]any     # avec quels paramètres
├── Policy      string             # décision: "allow", "deny", "human_approval"
├── PolicyRule  string             # quelle rule a matché
├── StatusCode  int                # code HTTP du backend
├── LatencyMs   int64              # temps de réponse
├── Error       string             # erreur éventuelle
└── Timestamp   time.Time          # quand

Store
├── mu       sync.RWMutex
├── entries  []Entry               # buffer circulaire
└── maxSize  int                   # taille max (10000 par défaut)
```

**Méthodes :**
- `Record(entry)` → ajoute une trace (auto-génère ID et timestamp)
- `Query(agent, tool, limit)` → filtre et retourne les dernières traces
- `Stats()` → compteurs agrégés (total, allowed, denied, errors)

---

## Graphe des dépendances

```
main.go
  ├── config     (charge le YAML)
  ├── registry   (catalogue de tools)
  ├── policy     (règles, dépend de config pour les types)
  ├── trace      (store de traces)
  ├── proxy      (handler HTTP, dépend de registry + policy + trace)
  └── mcp        (server + client + manager, dépend de registry + policy + trace + proxy)

proxy ← interface MCPForwarder ← mcp/manager
  (proxy définit l'interface, mcp l'implémente, pas d'import circulaire)
```

---

## Tests

Les tests sont dans des fichiers `_test.go` à côté du code source. Go les exclut automatiquement du build.

```bash
go test ./...              # tout tester
go test ./... -v           # verbose
go test ./... -race        # détecter les race conditions
go test ./proxy/ -v        # un seul package
go test ./policy/ -run TestEvaluateMCPNamespacedTools -v  # un seul test
```

Conventions :
- Nom de fonction : `TestNomDuTest(t *testing.T)`
- `t.Fatal("msg")` → stoppe le test immédiatement
- `t.Errorf("msg")` → note l'erreur mais continue
- `t.Helper()` → marque une fonction comme helper (meilleurs messages d'erreur)

---

## Go pour les Pythonistas

Guide de correspondance rapide pour lire le code agent-mesh quand on vient de Python/FastAPI.

### Types et structures

| Python | Go | Exemple agent-mesh |
|--------|-----|-------------------|
| `class Tool:` | `type Tool struct { ... }` | `registry/registry.go` |
| `def __init__(self):` | `func New() *Registry { ... }` | Pas de constructeur, on utilise des fonctions `NewXxx()` |
| `self.tools` | `r.tools` | Le receiver `r` remplace `self` |
| `dict[str, Tool]` | `map[string]*Tool` | `*Tool` = pointeur vers un Tool |
| `list[Tool]` | `[]*Tool` | Slice de pointeurs |
| `Optional[str]` | `string` + `omitempty` en JSON | Les strings vides = "pas de valeur" |
| `@dataclass` | `type Xxx struct` avec tags JSON | Les tags `` `json:"name"` `` contrôlent la sérialisation |
| `from pydantic import BaseModel` | `type Config struct` + `yaml:"field"` | `config/config.go` — les tags YAML font le parsing |

### Fonctions et méthodes

| Python | Go | Exemple |
|--------|-----|---------|
| `def get(self, name):` | `func (r *Registry) Get(name string) *Tool` | Le `(r *Registry)` est le receiver = `self` |
| `return tool, error` | `return tool, err` | Go retourne souvent 2 valeurs : résultat + erreur |
| `if err := ...; err != nil` | `try: ... except:` | Pas de try/catch en Go, on vérifie `err != nil` après chaque appel |
| `raise ValueError("...")` | `return fmt.Errorf("...")` | Pas de raise, on retourne l'erreur |
| `with open(f) as file:` | `f, err := os.Open(path); defer f.Close()` | `defer` = exécuté à la sortie de la fonction (comme `finally`) |

### Concurrence

| Python | Go | Exemple agent-mesh |
|--------|-----|-------------------|
| `threading.Thread` | `go maFonction()` | `go c.readLoop()` dans `mcp/client.go` |
| `threading.Lock()` | `sync.Mutex` | `writeMu` dans le transport stdio |
| `threading.RLock()` | `sync.RWMutex` | `mu` dans `registry/registry.go` — plusieurs lecteurs, un seul écrivain |
| `queue.Queue()` | `chan rpcResponse` | Les channels dans `mcp/client.go` pour send/receive entre goroutines |
| `asyncio.wait_for(coro, timeout)` | `select { case <-ch: case <-ctx.Done(): }` | `send()` dans `mcp/client.go` |
| `async with` | pas d'équivalent | Go n'est pas async/await, c'est des goroutines + channels |

### Interfaces

| Python | Go | Pourquoi |
|--------|-----|---------|
| `class MCPForwarder(Protocol):` | `type MCPForwarder interface { ... }` | `proxy/handler.go` |
| `def call_tool(self, ...):` | `CallTool(ctx, serverName, toolName, args)` | Méthode de l'interface |
| Pas besoin de `implements` | Pas besoin de `implements` | Si le type a les bonnes méthodes, il implémente l'interface automatiquement |

En Python tu ferais :
```python
class MCPForwarder(Protocol):
    def call_tool(self, server: str, tool: str, args: dict) -> Any: ...
    def server_statuses(self) -> Any: ...
```

En Go c'est :
```go
type MCPForwarder interface {
    CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (any, error)
    ServerStatuses() any
}
```

`Manager` dans `mcp/manager.go` implémente cette interface simplement parce qu'il a les bonnes méthodes — pas de déclaration explicite.

### HTTP

| Python/FastAPI | Go | Exemple agent-mesh |
|---------------|-----|-------------------|
| `@app.get("/tools")` | `case r.URL.Path == "/tools":` | `proxy/handler.go` — pas de décorateur, un switch sur le path |
| `FastAPI()` | `http.Server{Handler: handler}` | `main.go` |
| `uvicorn.run(app)` | `srv.ListenAndServe()` | `main.go` |
| `JSONResponse(data)` | `json.NewEncoder(w).Encode(data)` | `writeJSON()` dans `proxy/handler.go` |
| `request.json()` | `json.NewDecoder(r.Body).Decode(&req)` | `handleToolCall()` |
| `HTTPException(403)` | `writeJSON(w, 403, resp)` | Pas d'exceptions, on écrit la réponse directement |

### Config / YAML

| Python | Go | Exemple |
|--------|-----|---------|
| `pydantic.BaseModel` + `yaml.safe_load()` | `type Config struct` + `yaml.Unmarshal()` | `config/config.go` |
| Validation automatique par Pydantic | Pas de validation automatique | Les champs vides sont acceptés, il faut vérifier manuellement |
| `config.port or 9090` | `if cfg.Port == 0 { cfg.Port = 9090 }` | Défauts explicites dans `Load()` |

### Patterns récurrents dans le code

**Erreur handling (le plus déroutant pour un Pythonista) :**
```go
// Go : chaque appel retourne une erreur qu'on vérifie
result, err := doSomething()
if err != nil {
    return fmt.Errorf("context: %w", err)  // wrap l'erreur avec du contexte
}
```

Équivalent Python :
```python
try:
    result = do_something()
except Exception as e:
    raise RuntimeError(f"context: {e}") from e
```

**Defer (cleanup automatique) :**
```go
f, err := os.Open(path)
if err != nil { return err }
defer f.Close()  // sera appelé quand la fonction retourne, quoi qu'il arrive
// ... utiliser f ...
```

Équivalent Python :
```python
with open(path) as f:
    # ... utiliser f ...
```

**Select (attendre plusieurs événements) :**
```go
select {
case resp := <-ch:       // une réponse est arrivée
    return resp, nil
case <-c.done:           // la connexion est morte
    return rpcResponse{}, errors.New("connection lost")
case <-ctx.Done():       // timeout ou annulation
    return rpcResponse{}, ctx.Err()
}
```

Équivalent Python (approximatif) :
```python
done, pending = await asyncio.wait(
    [response_future, done_event.wait(), ctx_deadline],
    return_when=asyncio.FIRST_COMPLETED
)
```
