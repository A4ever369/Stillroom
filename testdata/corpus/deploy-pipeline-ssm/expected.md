# Expected knowledge — deploy-pipeline-ssm

Topic-level; not exact wording.

## Should learn (recall)

- Deploy model: pushing to the `dev` branch triggers CI that runs the test
  gate, builds and pushes an image to ECR, and deploys to the EC2 box via AWS
  SSM. A push is the whole deploy; it takes roughly 5 minutes.
- Runtime env lives in `/opt/acme-gateway/.env` on the instance (docker compose
  out of `/opt/acme-gateway`) and is NOT synced from the repo — runtime config
  changes must be applied on the box, not committed.
- The box is reached via AWS SSM Session Manager, not SSH (SSH is
  publickey-only and local keys are not authorized).

## Should NOT do (precision)

- Do NOT emit a fact about the developer's laptop having only AWS CLI v1. That
  is a property of one person's machine, not the project — it belongs in
  personal notes, not the team knowledge base. (Negative control for the
  local-machine-environment exclusion.)
- Do not restate the literal `aws ssm start-session` command as a fact; the
  durable knowledge is "use SSM, not SSH", not the exact invocation.

## Granularity

- About three facts (deploy pipeline, runtime env location, SSM access). One
  merged blob is too coarse; splitting each into per-command trivia is too fine.
