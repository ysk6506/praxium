package pgingest

import (
	"fmt"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// pgiBroken is a server whose tables were damaged: the calls that need a table that is gone or has another name fail with an
// error and leave nothing half done. Each case makes its own server, damages one table and calls what needs it.
type pgiBroken struct {
	name   string
	damage func(ref SourceRef) string
	calls  func(e *pgiEnv, kg graph.KGID, ref SourceRef, plan *ingest.Plan, lease ingest.RunLease) map[string]func() error
}

func pgiRename(table string) func(SourceRef) string {
	return func(SourceRef) string { return fmt.Sprintf(`ALTER TABLE %s RENAME TO %s_gone`, table, table) }
}

func pgiRenamePartition(table string) func(SourceRef) string {
	return func(ref SourceRef) string {
		return fmt.Sprintf(`ALTER TABLE "s_%d"."%s_p%d" RENAME TO "%s_p%d_gone"`, ref.KGNo, table, ref.SrcNo, table, ref.SrcNo)
	}
}

func pgiStage(e *pgiEnv, plan *ingest.Plan, lease ingest.RunLease, page ingest.StagePage) func() error {
	return func() error {
		sink, err := e.st.newSink(e.ctx, lease, plan)
		if err != nil {
			return err
		}
		return sink.Stage(e.ctx, page)
	}
}

// [REQ-700]
func TestPgiACallThatNeedsATableThatIsDamagedFailsWithAnError(t *testing.T) {
	full := pgiPage("ds_places", 0, "a")
	full.Keys = []ingest.StageKey{{No: 0, TypeNo: 1}}
	full.Ends = []ingest.StageEnd{{No: 0, Def: 1, Side: "from"}}
	full.Claims = []ingest.StageClaim{{No: 0, Scheme: "x", Value: "v"}}
	cases := []pgiBroken{
		{"the runs", pgiRename(pgschema.TableSyncRun), func(e *pgiEnv, kg graph.KGID, ref SourceRef, plan *ingest.Plan, lease ingest.RunLease) map[string]func() error {
			return map[string]func() error{
				"AcquireRun":   func() error { _, err := e.st.AcquireRun(e.ctx, lease.No, "o", time.Minute); return err },
				"RenewRun":     func() error { return e.st.RenewRun(e.ctx, lease) },
				"ReceivePlace": func() error { _, err := e.st.ReceivePlace(e.ctx, lease.No); return err },
				"AdvanceRun":   func() error { return e.st.AdvanceRun(e.ctx, lease, ingest.RunReceiving, ingest.RunComputing) },
				"EndRun":       func() error { return e.st.EndRun(e.ctx, lease, ingest.RunEnd{Stage: ingest.RunCanceled}) },
				"DetachSource": func() error { return e.st.DetachSource(e.ctx, kg, plan.Source.ID) },
				"Receive": func() error {
					_, err := e.st.Receive(e.ctx, lease, ingest.Intake{Reader: pgiAll(), Plan: plan})
					return err
				},
			}
		}},
		{"the sources", pgiRename(pgschema.TableSourceState), func(e *pgiEnv, kg graph.KGID, ref SourceRef, plan *ingest.Plan, lease ingest.RunLease) map[string]func() error {
			return map[string]func() error{
				"OpenRun": func() error {
					_, err := e.st.OpenRun(e.ctx, ingest.RunOpen{KG: kg, Source: plan.Source.ID, Owner: "o", Term: time.Minute})
					return err
				},
				"AttachSource": func() error { _, err := e.st.AttachSource(e.ctx, kg, "src_x", []string{"d"}); return err },
				"DetachSource": func() error { return e.st.DetachSource(e.ctx, kg, plan.Source.ID) },
			}
		}},
		{"the datasets", pgiRename(pgschema.TableSourceDataset), func(e *pgiEnv, kg graph.KGID, ref SourceRef, plan *ingest.Plan, lease ingest.RunLease) map[string]func() error {
			return map[string]func() error{
				"AttachSource": func() error { _, err := e.st.AttachSource(e.ctx, kg, "src_x", []string{"d"}); return err },
				"Receive": func() error {
					_, err := e.st.Receive(e.ctx, lease, ingest.Intake{Reader: pgiAll(), Plan: plan})
					return err
				},
			}
		}},
		{"the list of KGs", pgiRename("kg_catalog"), func(e *pgiEnv, kg graph.KGID, ref SourceRef, plan *ingest.Plan, lease ingest.RunLease) map[string]func() error {
			return map[string]func() error{
				"AttachSource": func() error { _, err := e.st.AttachSource(e.ctx, kg, "src_x", []string{"d"}); return err },
				"Receive": func() error {
					_, err := e.st.Receive(e.ctx, lease, ingest.Intake{Reader: pgiAll(), Plan: plan})
					return err
				},
			}
		}},
		{"the raw records of the source", pgiRenamePartition(pgschema.TableRawRecord), func(e *pgiEnv, kg graph.KGID, ref SourceRef, plan *ingest.Plan, lease ingest.RunLease) map[string]func() error {
			return map[string]func() error{"Stage": pgiStage(e, plan, lease, full)}
		}},
		{"the records received", pgiRenamePartition(pgschema.TableStageRecord), func(e *pgiEnv, kg graph.KGID, ref SourceRef, plan *ingest.Plan, lease ingest.RunLease) map[string]func() error {
			return map[string]func() error{"Stage": pgiStage(e, plan, lease, full), "Receive": func() error {
				_, err := e.st.Receive(e.ctx, lease, ingest.Intake{Reader: pgiAll(), Plan: plan})
				return err
			}}
		}},
		{"the keys received", pgiRenamePartition(pgschema.TableStageKey), func(e *pgiEnv, kg graph.KGID, ref SourceRef, plan *ingest.Plan, lease ingest.RunLease) map[string]func() error {
			return map[string]func() error{"Stage": pgiStage(e, plan, lease, full)}
		}},
		{"the ends received", pgiRenamePartition(pgschema.TableStageEnd), func(e *pgiEnv, kg graph.KGID, ref SourceRef, plan *ingest.Plan, lease ingest.RunLease) map[string]func() error {
			return map[string]func() error{"Stage": pgiStage(e, plan, lease, full)}
		}},
		{"the claims received", pgiRenamePartition(pgschema.TableStageClaim), func(e *pgiEnv, kg graph.KGID, ref SourceRef, plan *ingest.Plan, lease ingest.RunLease) map[string]func() error {
			full := full
			for id := range plan.SchemeNumbers() {
				full.Claims = []ingest.StageClaim{{No: 0, Scheme: id, Value: "v"}}
			}
			return map[string]func() error{"Stage": pgiStage(e, plan, lease, full)}
		}},
		{"the waits of the KG", func(ref SourceRef) string {
			return fmt.Sprintf(`DROP TABLE "s_%d".%s`, ref.KGNo, pgschema.TableRecordWait)
		}, func(e *pgiEnv, kg graph.KGID, ref SourceRef, plan *ingest.Plan, lease ingest.RunLease) map[string]func() error {
			e.exec(t, `UPDATE sync_run SET stage = 'canceled', ended_at = now() WHERE run_no = $1`, lease.No)
			return map[string]func() error{"DetachSource": func() error { return e.st.DetachSource(e.ctx, kg, plan.Source.ID) }}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := pgiOpen(t)
			plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
			kg := e.kg(t, 1)
			ref := e.attach(t, kg, plan)
			lease := e.open(t, kg, plan.Source.ID, "server-a")
			calls := c.calls(e, kg.ID, ref, plan, lease)
			e.exec(t, c.damage(ref))
			for name, call := range calls {
				if err := call(); err == nil {
					t.Errorf("%s: went through although %s are damaged", name, c.name)
				}
			}
		})
	}
}
