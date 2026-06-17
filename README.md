# Hivemind Agent

Agent déployé sur un cluster Swarm pour permettre à Hivemind de le piloter et de
le superviser **sans exposer le démon Docker** (l'agent compose vers le serveur,
mode `agent`). Voir le design : `hivemind/docs/agent-design.md`.

## Rôle

- Déployé en **service global** (une task par nœud).
- **Compose vers** le serveur Hivemind (dial-out → traverse NAT/firewall).
- S'enrôle via un **token à usage unique**, puis envoie des **heartbeats**.
- Détecte l'identité du nœud (`node_id`, rôle manager/worker, leader, `swarm_id`)
  pour que le serveur route : orchestration → un agent **manager**, données
  node-locales → l'agent du **bon nœud**.

> État : bootstrap (config + identité + enrôlement/heartbeat). Le plan de données
> (proxy API Docker + canal node + métriques) sera ajouté dans une phase suivante.

## Configuration (variables d'environnement)

| variable | requis | défaut | rôle |
|----------|:------:|--------|------|
| `HIVEMIND_SERVER` | oui | — | URL du hub agent Hivemind (dial-out) |
| `HIVEMIND_ENROLL_TOKEN` | au 1er boot | — | token d'enrôlement à usage unique |
| `HIVEMIND_AGENT_ID` | non | — | id d'agent déjà enrôlé (reconnexion) |
| `HIVEMIND_HEARTBEAT` | non | `15s` | intervalle des heartbeats |
| `HIVEMIND_INSECURE_SKIP_VERIFY` | non | `false` | désactive la vérif TLS (dev) |
| `DOCKER_HOST` | non | socket local | endpoint Docker |

## Build & run

```bash
go build ./...
HIVEMIND_SERVER=https://localhost:8080 HIVEMIND_ENROLL_TOKEN=xxx ./hivemind-agent
```

Déploiement cluster : voir `deploy/hivemind-agent.yml`.
