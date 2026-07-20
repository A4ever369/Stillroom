package distill

import (
	"testing"
	"time"

	"github.com/0xbeekeeper/stillroom/internal/ir"
)

func mkFact(id, body string) ir.Fact {
	return ir.Fact{
		ID: id, ObservedAt: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		Confidence: ir.ConfidenceHigh, Status: ir.StatusActive, Body: body,
	}
}

func TestSimilarExistingFlagsNearDuplicateChinese(t *testing.T) {
	existing := []ir.Fact{
		mkFact("deploy.acme.db-endpoint", "Acme 生产库入口是 pgbouncer,端口 6432,直连 5432 会被安全组拦。"),
		mkFact("ci.postgres.image", "CI 必须用 pgvector/pgvector:pg17 镜像。"),
	}
	dup := mkFact("deploy.acme.database-entry", "Acme 生产库的入口是 pgbouncer(端口 6432),直连 5432 被安全组拦截。")
	hits := SimilarExisting(dup, existing)
	if len(hits) != 1 || hits[0] != "deploy.acme.db-endpoint" {
		t.Fatalf("hits = %v, want the db-endpoint fact", hits)
	}
}

func TestSimilarExistingIgnoresSameIDAndUnrelated(t *testing.T) {
	existing := []ir.Fact{
		mkFact("deploy.acme.db-endpoint", "Acme 生产库入口是 pgbouncer,端口 6432。"),
	}
	// Same id → supersession, not duplication.
	same := mkFact("deploy.acme.db-endpoint", "Acme 生产库入口是 pgbouncer,端口 6432,新增说明。")
	if hits := SimilarExisting(same, existing); hits != nil {
		t.Fatalf("same-id should be skipped, got %v", hits)
	}
	// Unrelated body → no hit.
	other := mkFact("build.web.pnpm", "web 目录用 pnpm workspace,node 版本锁 22。")
	if hits := SimilarExisting(other, existing); hits != nil {
		t.Fatalf("unrelated should not hit, got %v", hits)
	}
}
