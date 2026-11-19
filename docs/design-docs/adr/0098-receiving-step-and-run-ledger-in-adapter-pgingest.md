---
title: ADR-0098 새 수집 흐름의 수신 단계와 실행 기록 — 새 유닛 `adapter.pgingest`
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH021
  - GOCOV001
  - TLOCK001
  - backend/internal/pgingest/ledger_test.go
  - backend/internal/pgingest/receive_test.go
  - backend/internal/pgingest/sink_test.go
  - backend/internal/pgingest/source_test.go
  - backend/internal/pgingest/rules_test.go
  - backend/internal/ingest/runs_test.go
touches_protected:
  - harness/config/architecture.yaml
  - docs/product-specs/requirements-ingestion.md
  - harness/config/test-lock.json
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/ingestion-at-scale.md
  - docs/product-specs/requirements-ingestion.md
decision_hash: f828764e7b654db3
---
# ADR-0098 새 수집 흐름의 수신 단계와 실행 기록 — 새 유닛 `adapter.pgingest`

## 맥락

ADR-0086이 수집을 수신, 원본 차이, 식별, 다시 계산, 적용, 변동 값, 정리의 단계로 다시 설계했고, ADR-0095가 그 테이블(`stage_record`, `stage_key`, `stage_end`, `stage_claim`, `sync_run`, `source_state`, `source_dataset` 등)을 스키마 계획에 더했다. 그 테이블을 쓰는 코드는 아직 없다. 이 변경은 1단계(수신)와 그것이 기대는 실행 기록의 최소한, 소스를 KG에 붙이고 떼는 일을 PostgreSQL 위에 짓는다. 원본의 차이(2단계), 식별(3단계), 상태 기계는 뒤의 변경이다.

## 결정

1. **새 유닛 `adapter.pgingest`**(`backend/internal/pgingest`). `may_import`는 `core.kernel`, `core.model`, `core.graph`, `core.ingest`, `adapter.pgconn`, `adapter.pgschema`이고 테스트만 `adapter.pgstore`(KG의 스키마를 만든다)를 import한다. `lib/pq`를 import하지 않는다. `COPY`는 `COPY ... FROM STDIN` 문장을 트랜잭션에서 준비하고 행마다 `Exec`한 뒤 인자 없는 `Exec`으로 끝내는 `database/sql`의 방식이다(원형에서 확인한 방법). 셀의 타입은 문자열, 바이트(bytea의 16진 글자), 정수, 불리언, NULL이고 배열과 jsonb는 PostgreSQL의 글자 형태로 문자열에 담는다. 테이블과 열의 이름은 `pgschema`의 상수와 함수만 쓴다(KG 목록 `kg_catalog`만 `pgconn` 마이그레이션의 것이라 이 유닛의 상수로 둔다).
2. **실행 기록의 포트 `ingest.RunLedger`**(`core.ingest`의 `runs.go`). 메서드는 `OpenRun`, `AcquireRun`, `RenewRun`, `ReceivePlace`, `AdvanceRun`, `EndRun`이고 이번에 쓰지 않는 것은 만들지 않았다. 상태 기계가 이 포트만으로 단계를 진행한다: 단계는 `RunStage`, 갈 수 있는 길은 `CanAdvance`(수신에서 계산, 계산에서 확인 대기나 적용, 확인 대기에서 계산, 적용에서 변동 값, 변동 값에서 정리)이고 끝은 `EndRun`이다. 오류 코드는 `ingest_run_open`, `ingest_run_lease_lost`, `ingest_run_held`, `ingest_run_stage`, `ingest_kg_gate_taken`이다.
3. **임대와 시도 번호.** 실행에 쓰는 모든 문장의 조건은 `run_no = $1 AND attempt = $2 AND stage NOT IN (끝난 단계)`이다. 임대를 넘겨받으면(`AcquireRun`) 시도 번호가 하나 오르고, 그 뒤 옛 시도의 쓰기는 한 행도 바꾸지 못한다. 임대의 만료는 DB의 시계(`now() + make_interval(secs => ...)`)로 정한다. 관문은 ADR-0095의 부분 유일 인덱스 둘이다: 열기에서 `sync_run_one_open_per_source`의 위반은 `ingest_run_open`이고, 계산으로 옮길 때 `sync_run_one_working_per_kg`의 위반은 `ingest_kg_gate_taken`이다(실행은 수신에 머문다).
4. **수신 트랜잭션의 순서(페이지 하나 = 트랜잭션 하나).**
   1. 실행 행을 시도 번호를 조건으로 갱신해 자리(데이터셋, 페이지, 토큰, 마지막인지)와 임대를 쓴다. 한 행도 바뀌지 않으면(시도가 다르거나 수신가 아니면) 아무것도 쓰지 않고 오류를 낸다.
   2. 페이지의 자연 키들로 소스의 `raw_record` 파티션에서 원본 해시와 운명을 한 번에 찾는다(인덱스만 읽는다). 새로 옴(키가 없다), 바뀜(해시가 다르거나 걸러짐이 바뀌었다), 그대로를 정한다. 안 보임, 돌아옴, 유예, 중복은 2단계의 일이다.
   3. `stage_record`에 모든 행을, `stage_key`, `stage_end`, `stage_claim`에 새로 왔거나 바뀐 행의 것만 테이블마다 `COPY` 한 번으로 넣는다. 한 페이지는 행 1만 개까지다.
   4. 커밋한다. 어느 단계든 실패하면 자리도 남지 않는다.
5. **테이블에 더한 것은 없고, 모자란 자리는 약속으로 채웠다.** ADR-0095의 테이블에 더하기만으로 되는 열이 있지만, 생성기 버전을 올리면 잠긴 테스트 둘(`TestControlReservesThePartitionPrefix`의 버전 단언, `TestIngestDiffFollowsTheStepFromTheGeneratorBeforeByAddingOnly`)이 바뀌어 책임자의 승인 없이 할 수 없다. 그래서 **생성기 버전은 3 그대로**이고 아래의 약속을 이 ADR이 기록한다. 2단계가 이 약속을 읽는다. 약속이 쓰기 불편하면 뒤의 변경이 열을 더하면서 생성기 버전을 올린다.
   - 페이지의 문제(읽지 못한 줄, 머리글이 바뀜, 끝까지 읽지 못한 데이터셋)는 `stage_record`의 행 번호 -1인 행 하나의 문제 칼럼에 둔다. 레코드의 문제는 그 레코드 행에 둔다. 문제 종류는 `pgschema.ProblemKinds`의 고정 목록에 있어야 한다.
   - 걸러진 행(피드의 규칙이 거른 행)의 키, 끝 키, 주장은 피드나 정의 번호 n을 `^n`(음수)으로 저장한다. 걸러지지 않은 것은 n이다.
   - 식별자 체계의 번호는 모델에 나열된 순서의 1부터의 번호다(`Plan.SchemeNumbers`). 같은 정의 세트는 같은 번호를 준다.
   - 키의 순서 칼럼은 피드 안의 순서(`Ord`)다. 링크의 끝은 방향 코드 `RefRoleLinkFrom`(2)과 `RefRoleLinkTo`(3)이다. 자리는 시트의 범위(`Where`)이고 없으면 줄 번호다.
   - 이어 읽기 토큰은 NULL(없음), `t:`와 토큰의 base64, 데이터셋의 마지막 페이지이면 `last`다.
6. **수신를 돌리는 `Receive`.** 실행이 수신 단계이고 임대가 내 것인지, 그 소스의 실행인지 확인하고, 한 트랜잭션에서 자리를 지우고 소스의 수신 파티션을 `TRUNCATE`한 뒤(불변식 I3) `Intake`를 이 `Sink`로 돌린다. 읽기가 끝나면 데이터셋마다의 건수(페이지, 레코드, 끝까지 읽었는지와 이유)를 실행의 건수 `receive`에 적고 단계를 계산으로 넘긴다. 읽다 만 데이터셋은 건수와 그 마지막 페이지의 문제로 남아 2단계가 통째로 뺄 수 있다. 관문이 잡혀 있으면 받은 행을 둔 채 수신에 머물고 오류를 돌려준다(호출한 쪽이 관문이 비면 단계를 넘긴다. `Receive`를 다시 부르면 처음부터 받는다). 토큰으로 이어 가는 길은 자리(`ReceivePlace`)만 남겼다.
7. **소스를 KG에 붙이고 뗀다.** `AttachSource`는 한 트랜잭션에서 소스의 상태 행과 데이터셋 행을 만들고(새 소스는 "전체 다시 수신"과 "전체 계산" 표시가 켜진다) 파티션 다섯을 만든다. 되풀이해도 있는 것은 그대로이고, 새 데이터셋을 더할 수 있고, 만들다 만 파티션은 붙인다. 같은 소스의 붙이기는 어드바이저리 잠금으로 줄을 세운다. `DetachSource`는 끝나지 않은 실행이 있으면 `ingest_run_open`으로 거절하고, 분할되지 않은 테이블(`record_ref`, `record_wait`, `record_problem`)의 그 소스 행을 1만 행씩 트랜잭션으로 지운 뒤 한 트랜잭션에서 파티션을 지우고 소스의 상태를 지운다(데이터셋과 실행이 따라 지워진다).
8. **KG를 지울 때 소스를 먼저 뗀다(REQ-684).** `pgstore`의 `DeleteKG`와 `DropStorageSchema`가 스키마를 삭제 전에 소스를 하나씩, 트랜잭션마다 하나씩 뗀다. `adapter.pgstore`는 `adapter.pgingest`를 import할 수 없어(어댑터끼리의 규칙) 같은 일을 `pgstore`의 `kgsources.go`가 `pgschema.DropSourcePartitionSteps`로 한다. 끝나지 않은 실행이 있어도 KG가 사라지는 중이라 막지 않는다. 스키마를 지운 뒤 남은 소스의 상태 행(스키마 밖의 공용 테이블)은 지운다. 같은 로직이 두 곳에 있다는 것은 아래 위험에 적었다.

## 측정 (PostgreSQL 18.7, 표준 모델, 기본 설정 `max_locks_per_transaction = 64`)

| 일 | 잠금 수(`pg_locks`의 줄) |
|---|---|
| 소스를 붙인다(`AttachSource`의 트랜잭션 전체) | 76 (파티션 문장만 44) |
| 소스를 뗀다(`dropTx`) | 94 (파티션 문장만 76) |
| 소스 30개가 붙은 KG의 스키마를 한 트랜잭션에 지운다 | 6,630 (기본 잠금 테이블 6,400을 넘는다. 이 서버는 연결을 더 허용해 통과했다) |
| 소스를 먼저 떼고 지운다(`DeleteKG`) | 소스마다 94 이하, 스키마의 삭제는 소스가 없는 KG와 같다 |

소스 30개가 붙은 KG를 기본 설정에서 지우는 일을 테스트가 지킨다(`TestPgiPuttingASourceToAKG...`).

## 이유

- 수신를 실행 기록과 같은 트랜잭션에 묶어야 죽은 서버의 자리가 받은 행과 어긋나지 않는다.
- 시도 번호를 조건으로 건 갱신은 임대의 시계 비교가 틀려도 늦게 깬 서버가 쓰지 못하게 한다. 임대는 누가 하는지만 정한다.
- 소스마다의 파티션은 `TRUNCATE`로 비우기만 하고 `UPDATE`가 없으므로 불변식 I3을 지킨다.
- 생성기를 올리지 않고 약속으로 채우면 잠긴 테스트를 건드리지 않고, 이미 있는 KG를 맞출 필요가 없다.

## 대안과 기각 사유

- **생성기 버전을 4로 올려 열을 더한다.** 잠긴 테스트 둘이 바뀐다. 이번 묶음의 지시가 잠긴 테스트의 변경을 금지했고, 약속으로 필요한 것을 모두 담을 수 있다.
- **`pgingest`가 KG 삭제를 맡는다.** 어댑터끼리 import하지 못하고, 둘을 합치면 저장 어댑터가 수집을 알게 된다. 그래서 `pgstore`에 같은 일을 하는 작은 함수를 둔다.
- **`lib/pq`의 `CopyIn`을 쓴다.** 규칙상 `lib/pq`는 `pgconn`과 `pgstore`만 import한다. `COPY`의 글자를 준비하고 행마다 `Exec`하는 것이 `CopyIn`이 하는 일의 전부다.
- **페이지마다 `INSERT`를 여러 개 한다.** 문장 수가 행 수에 비례해 왕복이 늘고, 원형이 `COPY`로 잰 처리량을 쓸 수 없다.
- **임대의 시계를 서버의 시각으로 쓴다.** 서버끼리 시계가 다르다. DB의 시각 하나로 정한다.

## 결과

- 수집의 새 흐름이 1단계까지 PostgreSQL에서 돈다. 옛 수집 코드는 건드리지 않았다(상태 기계와 정리의 변경에서 지운다).
- **뒤의 변경이 알 것**: 2단계는 위의 약속(행 번호 -1, `^n`, 체계 번호, 방향 코드, 토큰의 글자)을 읽고, 읽다 만 데이터셋은 `sync_run.counts`의 `receive`에서 찾는다. 수신가 `computing`으로 넘기지 못한 실행은 `receiving`에 받은 행과 함께 남는다. `Receive`가 설정한 "전체 다시 수신" 표시를 끄는 것은 뒤의 단계다.
- **남는 위험**: (1) 소스를 떼는 로직이 `pgingest`와 `pgstore`에 둘 있다(같은 `pgschema` 함수를 쓰고, `pgingest`의 테스트가 `DeleteKG`로 확인한다). (2) 약속이 열의 이름만으로 드러나지 않아 2단계가 어기면 조용히 틀린다. 약속은 `Sink`의 문서와 이 ADR에 있다. (3) 관문이 잡혀 `computing`으로 못 넘긴 실행의 받은 행은 다음 `Receive`가 다 비우므로, 이어 가려면 호출한 쪽이 단계 넘기기만 다시 불러야 한다.
