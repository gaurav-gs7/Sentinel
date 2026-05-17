# Incident Flow

1. Alertmanager fires an SLO or Kubernetes health alert.
2. On-call opens the generated runbook.
3. The service status endpoint shows current SLO and deployment state.
4. The incident enrichment workflow correlates the alert with service owner, tier, SLO state, recent deployment context, and runbook metadata.
5. If a canary breaches the health gate, Sentinel records rollback intent through a workflow-backed rollout step.
6. GitOps or the local runner restores the last known good image or manifest.
7. The workflow event trail, incident timeline, and postmortem draft become RCA evidence.
8. The incident is reviewed and the generated runbook is updated with learnings.
