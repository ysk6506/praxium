---
title: ADR-0095 새 수집 흐름의 테이블을 스키마 계획에 더한다
kind: decision
status: active
enforced_by:
  - GOARCH010
  - TLOCK001
  - backend/internal/pgschema/ingest_test.go
  - backend/internal/pgstore/ingest_tables_test.go
  - backend/internal/pgstore/ingest_runs_test.go
  - backend/internal/pgstore/schema_startup_test.go
  - backend/cmd/praxium/store_adjust_test.go
  - backend/internal/pgconn/migrate_cancel_test.go
touches_protected:
  - docs/design-docs/adr/0085-new-kg-storage-implementation-decisions-and-the-schema-plan-unit.md
  - docs/product-specs/requirements-ingestion.md
  - harness/config/test-lock.json
updates_docs:
  - docs/design-docs/ingestion-at-scale.md
  - docs/design-docs/ingestion-at-scale-identity.md
  - docs/design-docs/storage-shape.md
  - docs/product-specs/requirements-ingestion.md
amends:
  - adr: "0085"
    what: 결정 6의 더하는 변경만 받는다는 규칙에서 생성기 버전이 바뀌면 거절한다는 것(더하기만인 알려진 단계는 따른다)
amends_tests:
  - go:backend/internal/pgschema::TestControlReservesThePartitionPrefix
test_change_reason: 생성기 버전이 2에서 3이 되어, 버전이 2라고 단언하던 테스트가 3이라고 같은 것을 확인한다. 단언은 줄지 않았고 파티션 접두어의 예약을 확인하는 앞부분도 그대로다. 옛 값과 새 값은 아래 표에 있다.
decision_hash: 4104eeeec0af9351
amended_by:
  - adr: "0098"
    what: 결정 3과 4의 수신 테이블 세 곳에 held 열을 더하고 생성기 버전을 4로 올리는 것
  - adr: "0101"
    what: 결정 3의 `stage_volatile`의 `entry`가 `object_state`의 `prov`에 들어갈 항목이라는 것(값은 열로 가고 시각 키는 `t`다)
  - adr: "0102"
    what: 결정 3과 4의 `record_wait`, `calc_record`, `record_problem`, `calc_problem`에 열을 더하거나 NULL을 허용하고, `stage_record`에 인덱스를 더하고, 생성기 버전을 6으로 올리는 것
---
# ADR-0095 새 수집 흐름의 테이블을 스키마 계획에 더한다

## 맥락

ADR-0086이 수집을 수천만 객체의 규모로 다시 설계했고(수집, 계산, 적용, 변동 값, 정리), ADR-0085가 준비 테이블의 이름(`stage_`, `calc_`, `ingest_diff`)을 예약했다. ADR-0094로 저장은 새 KG 저장 하나가 되었다. 새 흐름의 SQL(수집, 식별, 차이, 적용)을 쓰는 뒤의 변경들이 테이블과 열의 이름을 글자로 따로 적지 않으려면 테이블이 먼저 있어야 한다. 이 결정은 **테이블과 그 이름·열을 주는 함수까지**다. 테이블을 쓰는 코드와 새 유닛 `adapter.pgingest`는 뒤의 변경이다.

## 결정

1. **영구 테이블**은 KG의 저장 스키마에 둔다.

| 테이블 | 열 | 키와 인덱스 |
|---|---|---|
| `raw_record` (소스마다의 파티션) | `src_no`, `dataset_no`, `key`, `raw_hash`(32바이트), `fields text[]`(선언한 컬럼의 순서), `decl_rev`, `opened_seq`, `fate`(쓰임 0, 문제 1, 걸러짐 2, 안 쓰임 3) | 기본키 (`src_no`, `dataset_no`, `key`)에 `raw_hash`와 `fate`를 얹는다(`INCLUDE`). 페이지의 키로 해시와 운명을 찾는 일이 인덱스만 읽는다 |
| `record_wait` | `src_no`, `dataset_no`, `key`, `kind`(안 보임 0, 걸러짐 1), `runs`, `first_at` | 기본키에 `kind`가 든다 |
| `record_ref` | `src_no`, `dataset_no`, `key`, `role`(식별 1, 링크의 From 끝 2, To 끝 3, 식별자의 주장 4), `def_no`, `sub`, `lookup_hash`, `target_no`(NULL이면 미해결), `state`(해결 0, 미해결 1, 진 주장 2, 걸러짐 3), `retirable` | 기본키 (`src_no`, `dataset_no`, `key`, `role`, `def_no`, `sub`). 인덱스 넷: `target_no`(식별만), `target_no`(링크의 From 끝만), (`role`, `def_no`, `lookup_hash`)(`lookup_hash`가 있는 줄만), (`src_no`, `dataset_no`, `key`)(`state`가 해결이 아닌 줄만: 미해결 목록) |
| `record_problem` | `src_no`, `dataset_no`, `key`, `def_no`, **`field`**, `kind`(고정 목록. `CHECK`), `reason` | 인덱스 (`src_no`, `dataset_no`, `key`) |
| `object_key` (이미 있다) | ADR-0089의 열 그대로 | **모자란 것이 없다.** 열(`type_no`, `ctx_no`, `sig_hash`, `obj_no`, `bound_seq`, `sig`)과 인덱스(`obj_no`, `bound_seq`)가 설계와 같다. 등록부의 `first_key`와 타입 테이블의 `row_hash`도 이미 있다 |

   문제의 종류는 `ingest.Problem*`의 열다섯 낱말과 문맥 키가 더하는 둘(`context_parent_missing`, `missing_name`)이다(`pgschema.ProblemKinds`). 테이블이 다른 낱말을 거절한다. 같은 목록이 `core.ingest`에도 있으므로 두 목록이 같은지는 둘을 함께 아는 뒤의 변경(`adapter.pgingest`)의 테스트가 본다.

2. **준비 테이블**은 **로그 있는 테이블**이다(`UNLOGGED`가 아니다). 넣기만 하고(I3) 비우기는 `TRUNCATE`다.
   - **소스마다의 파티션(수신용)**: `stage_record`(`src_no`, `dataset_no`, `page`, `row_no`, `key`, `place`, `filtered`, `change_kind`(새로 옴 0, 바뀜 1, 그대로 2), `raw_hash`, `fields`, `problems jsonb`), `stage_key`(행의 자리, `feed_no`, `key_ord`, `type_no`, `sig_hash`, `cand_ref`, `anc_types`, `anc_sigs`), `stage_end`(행의 자리, `def_no`, `side`, `lookup_hash`), `stage_claim`(행의 자리, `scheme_no`, `feed_no`, `value_hash`, `value`). 키가 없어서 인덱스가 없다.
   - **KG마다 한 벌(계산용)**: `calc_record`(`rid`는 들어온 순서의 번호, 소스, 데이터셋, 키, 필드, 해시, `opened_seq`, `fate`, `why`: 바뀜 0, 미해결 1, 확장 2), `calc_wait`, `calc_key`, `calc_end`, `calc_claim`(그대로인 레코드에서 다시 만든 것과 바뀐 레코드에서 옮긴 것. `rid`로 가리킨다), `calc_bind`(`rid`, `key_ord`, `obj_no`, `is_new`, `round_no`), `calc_newkey`, `calc_dropped`, `calc_object`, `stage_object`, `stage_link`, `stage_volatile`, `stage_ident`, `calc_problem`, `calc_ref`, `ingest_diff`.
   - `stage_object`와 `stage_link`는 적용이 복사하는 타입 테이블과 `link`의 열을 그대로 가진다(`min_grade`, `source_refs`, 내용 해시, 행 합까지). `stage_volatile`은 `object_state`의 `prov`에 들어갈 속성 항목(`entry`)을 가진다.
   - `ingest_diff`는 `ord`(저장된 순서), `chunk_no`(저장된 청크 번호), `kind`(**객체 0, 키 1, 링크 2, 식별자 3**), `type_no`, `entity_no`, `ref`, `change_kind`(0 더함, 1 바뀜, 2 물러남. 변경 항목과 같은 코드), 옛 해시와 새 해시, 키 줄이 묶는 키의 `ctx_no`와 `sig_hash`다. 청크 번호로 고르는 인덱스 하나를 둔다.

3. **공용 테이블**은 새 마이그레이션 `pgconn/migrations/0006_ingest_runs.sql`이 더한다(번호 붙은 기존 파일은 고치지 않았다. 더하기만이라 저장 형식 번호는 그대로 2다).
   - `sync_run`: `run_no`(시퀀스), `kg_id`, `src_no`, 종류(`full`, 자리만 있는 `incremental`과 `stream`), 방아쇠, 적용 방식, **단계**(`receiving`, `computing`, `awaiting_confirmation`, `applying`, `volatile`, `settling`과 끝 `succeeded`, `failed`, `canceled`, **`discarded`**), 이어 갈 자리(수신의 데이터셋·페이지·토큰, 계산·변동 값·정리의 일과 구간), 건수, 적용 id, 정의 세트의 해시, 변경 지문, 소유자와 **시도 번호**와 임대(`lease_until`), 시각, **끝난 이유의 세 갈래**(`end_class`: `config`, `transient`, `product`. 실패한 실행만 갖는다)와 코드와 이유. 끝난 단계와 `ended_at`은 서로 있어야 한다(`CHECK`).
   - **관문은 부분 유일 인덱스 둘이다.** `sync_run_one_open_per_source`: 끝나지 않은 실행은 소스마다 하나. `sync_run_one_working_per_kg`: `computing`, `applying`, `volatile`, `settling`인 실행은 KG마다 하나(확인 대기는 관문을 놓으므로 들지 않는다).
   - `source_state`(`src_no`, KG, 소스 id, "전체 다시 수신"과 "전체 계산" 표시, 마지막으로 적용한 정의 세트), `source_dataset`(`dataset_no`, `src_no`, 캡처 해시, 선언의 리비전, 커서). 소스와 데이터셋의 번호는 DB 전체에서 하나뿐인 시퀀스(`source_no_seq`, `dataset_no_seq`)에서 받는다. 새 소스는 처음 실행 전이므로 두 표시가 켜져 있다. 소스를 지우면 데이터셋과 실행 기록이 따라 지워진다.

4. **생성기 버전을 3으로 올리고, `Diff`는 알려진 더하기 단계를 따른다.** `Build`가 내는 계획에 위의 테이블과 인덱스가 든다(그래서 `Diff`와 매니페스트가 따라온다). 생성기 버전이 2인 KG(이 변경 앞에 만든 KG)는 `Diff`가 "버전 3이 더한 고정 테이블의 만들기"(`generations.go`의 `generatorAdditions`)를 단계로 낸다. 버전 1이거나 이 빌드보다 새롭거나, 이전 버전이 만든 것을 바꾼 버전은 지금처럼 거절한다(`pgschema_not_additive`). 그래서 `AdjustStorageSchema`가 그대로 이 추가를 처리하고 KG를 닫지 않는다(테이블을 새로 만들 뿐이라 KG의 다른 테이블에 거는 잠금이 없다. 테스트가 `object`와 `link`에 `ROW EXCLUSIVE` 잠금을 쥔 연결이 열린 채로 맞춘다). 이것이 ADR-0085 결정 6의 "더하는 변경만 받는다"를 고치는 점이다: 생성기가 바뀌면 거절하던 것을, 바뀐 것이 고정 테이블의 더하기뿐이고 그 단계를 `Diff`가 아는 버전이면 따른다.

5. **이름과 열은 `pgschema`가 준다.** 테이블 이름(`Table*`), 열 이름(`Col*`), 코드(`Fate*`, `Wait*`, `RefRole*`, `Ref*`(상태), `Receive*`, `CalcWhy*`, `DiffKind*`, `Run*`, `EndClass*`), 문제의 종류(`ProblemKinds`), 테이블의 목록(`IngestTables`, `SourceTables`, `ReceiveTables`, `ComputeTables`), 열의 목록(`ColumnsOf`, 공용 테이블은 `SyncRunColumns`, `SourceStateColumns`, `SourceDatasetColumns`), 이름의 짝(`TableRef`, `SourcePartition`). 공용 테이블은 마이그레이션이 SQL 파일이라 `pgschema`가 읽지 못하므로, 열 목록이 마이그레이션이 만든 열과 같은지를 PostgreSQL 테스트가 견준다.

6. **소스의 파티션.** 소스를 KG에 붙일 때 `SourcePartitionSteps`가 낸 문장으로 `raw_record`와 수신용 넷의 파티션을 만든다(`<테이블>_p<src_no>`). **테이블을 `LIKE ... INCLUDING ALL`로 따로 만들고 `ATTACH PARTITION`으로 붙인다.** `CREATE TABLE ... PARTITION OF`는 부모에 가장 센 잠금을 걸어 다른 소스가 받는 중이면 기다리지만, 붙이기는 그렇지 않다(테스트가 다른 소스의 열린 쓰기 트랜잭션 옆에서 붙인다). `raw_record`의 파티션은 채움 비율과 청소 설정을 받는다(파티션된 테이블은 저장 설정을 갖지 못해 파티션을 만들 때 준다). 소스를 떼는 `DropSourcePartitionSteps`, 수신을 비우는 `TruncateReceiveSQL`, 계산을 시작할 때의 `TruncateComputeSQL`(`RESTART IDENTITY`)도 준다. 실행하는 것은 뒤의 유닛이다.

7. **다이제스트 줄의 바이트 배치**(책임자 판단 2)는 `pgschema/ingest_digest.go` 한 곳에 있다. 줄 = 종류 1바이트, 타입 번호 2바이트, 번호 8바이트, 변화 1바이트, 옛 해시 32바이트, 새 해시 32바이트(없는 해시는 0 32바이트)로 **76바이트, 큰 쪽이 앞(big-endian)**이다. 청크의 다이제스트는 `ord` 순서로 이어 붙인 줄의 SHA-256이고, 사슬은 SHA-256(꼬리표 `praxium.batch.v2` 16바이트, 앞의 사슬 32바이트, 청크 32바이트)이다(앞의 사슬이 없으면 0 32바이트). 키 줄의 새 해시는 SHA-256(타입 번호 2바이트, `ctx_no` 8바이트, `sig_hash` 16바이트)이라 다이제스트가 묶이는 키까지 덮는다. Go의 기준 구현(`DiffLine`, `ChunkDigest`, `ChainDigest`, `InputDigest`, `KeyRowHash`)과 같은 줄을 SQL로 만드는 식(`DiffLineExpr`, `KeyRowHashExpr`, `ChunkDigestSQL`)이 있고, 고정 값 테스트와, 같은 줄들에서 SQL과 Go가 같은 값을 내는 PostgreSQL 테스트가 있다.

8. **`max_locks_per_transaction`.** 잠금 테이블은 연결 모두가 나눠 쓰고 크기가 설정값 × (연결 수 + 준비된 트랜잭션 수)다. 한 트랜잭션은 거기서 필요한 만큼 가져간다. 측정은 PostgreSQL 18.7(표준 모델, 통계 포함, 소스 없음)에서 `pg_locks`의 줄 수다.

| 일 | 이 변경 앞 | 이 변경 뒤 | 기본 설정의 잠금 테이블(64 × 100 = 6,400)에서 |
|---|---|---|---|
| KG 하나를 만든다(한 트랜잭션) | 2,213 | 2,357 | 37% |
| KG 하나의 스키마를 지운다(한 트랜잭션) | 4,283 | 4,590 | 72% |
| 소스 하나를 붙인다 | | 44 | |
| 소스 하나를 뗀다 | | 76 | |
| 소스가 9개 붙은 KG의 스키마를 지운다 | | 5,202 | 81%(소스마다 약 68) |

   **소스가 없는 표준 모델의 KG는 기본값 64로 된다**(만들기와 삭제 모두 한 트랜잭션으로 들어간다). 설정을 올리지 않아도 된다. 테스트는 둘 다 기본 잠금 테이블의 80%를 넘지 않는지 본다. **그러나 소스가 붙은 KG를 한 트랜잭션으로 지우면 소스 26개쯤에서 기본값에 닿는다**(4,590 + 68 × 소스 수 > 6,400). 책임자의 판단 8(한 KG에 소스 수백)대로 소스가 300개이면 약 25,000개가 필요해 설정을 올리는 것은 답이 아니다. 그래서 **소스를 붙이는 뒤의 변경은 KG의 삭제가 소스의 파티션을 하나씩(트랜잭션마다 하나, 76개 잠금) 먼저 떼고 스키마를 지우게 해야 한다.** 이 변경은 소스를 붙이는 코드가 없어 `DeleteKG`를 고치지 않았다. 운영 안내에 적을 값은 따로다: 권고 `max_locks_per_transaction = 128`(연결 100개에서 잠금 테이블 12,800개, 표준 모델 KG의 삭제가 그 36%). 값을 올려서 소스 수백 개의 삭제를 한 트랜잭션에 넣지 않는다.

9. **옛 수집 코드는 건드리지 않았다.** `v2names.go`와 기존 테스트 파일도 그대로다. 잠긴 테스트에서 바뀐 것은 아래 한 건이고, 테이블·인덱스·통계의 개수를 고정한 기존 단언은 모두 그대로 통과한다(그 단언들은 계획의 테이블 목록에서 개수를 읽는다).

10. **서버가 시작할 때 옛 생성기의 KG를 맞춘다.** 이 변경이 생성기 버전을 올리므로, 맞추는 제품의 길이 없으면 main에서 이 변경 앞에 만든 KG가 모두 스키마 어긋남(D19)으로 거절된다. 그래서 서버는 시작할 때(마이그레이션과 저장 형식 관문 뒤, `ResumeWork`와 같은 자리인 `cmd/praxium/store.go`의 postgres 경우, 이어 하기보다 앞) `pgstore.Store.AdjustOldSchemas`를 부른다. 이 함수는 생성기 버전이 이 빌드보다 **낮은** KG마다 `AdjustStorageSchema`를 부른다. 규칙은 다음과 같다.
    - **더하기만 맞춘다.** 모델, 번호 매김, 검색 집합의 해시가 이 빌드의 모델과 다른 KG와, `Diff`가 더하기가 아닌 차이(`pgschema_not_additive`)를 내는 KG는 맞추지 않고 지금처럼 D19로 거절한 채 두며, 이유를 로그에 남긴다.
    - **여러 서버가 함께 시작해도 된다.** ADR-0087 결정 6의 `building` 상태를 그대로 쓴다. 다른 실행이 맞추는 중이고 오래되지 않았으면 건너뛰고 로그만 남긴다(30분 동안 말이 없던 실행은 지금처럼 넘겨받는다).
    - **한 KG 때문에 서버가 멈추지 않는다.** 한 KG를 못 맞추면 경고를 남기고 서버는 뜬다(`ResumeWork`와 같은 원칙). 그 KG는 맞춰질 때까지 D19로 거절되고, 다른 KG는 계속 맞춘다.
    - **이 빌드보다 높은 생성기의 KG는 건드리지 않는다**(지금의 거절 그대로).
    테스트는 서버가 생성기 2의 KG로 시작해 3이 되고 질의가 열리는 것(`cmd/praxium`), 더하기가 아닌 차이는 맞추지 않는 것, `building`이 잡혀 있으면 건너뛰는 것, 실패해도 시작이 계속되고 더 높은 생성기는 그대로인 것(`pgstore`)을 확인한다. 요구는 REQ-685다.

## 문서와 다르게 한 곳

설계 문서끼리, 또는 문서와 지금의 코드가 어긋나는 곳은 지금 코드와 ADR-0089, ADR-0094를 기준으로 맞췄다.

| 곳 | 문서 | 이 변경 | 이유 |
|---|---|---|---|
| `ingest_diff`의 종류의 순서 | 수집의 규모 설계 5.2절은 "키, 객체, 링크, 식별자"로 늘어놓고 설계 v2 6.1절은 "객체 → 키 → 링크 → 식별자"의 적용 순서를 말한다 | 객체 0, 키 1, 링크 2, 식별자 3 | 순서가 (종류, 타입 번호, id)이므로 종류의 번호가 곧 적용 순서다. 적용 순서를 말한 쪽을 따르고 5.2절을 고쳤다 |
| `record_ref`의 인덱스 | 셋(객체에서 기여 레코드로, 끝 키와 주장으로 찾기, 미해결 목록) | 넷. 링크의 From 끝을 `target_no`(링크 번호)로 찾는 부분 인덱스를 더했다 | 폐기되는 객체에 닿은 링크를 만든 레코드를 찾는 것(영향 집합 확장 (사))에 필요하다. 원형도 같은 인덱스를 더했다 |
| `record_ref`의 찾기 인덱스 | (`role`, `def_no`, `lookup_hash`) | `lookup_hash`가 있는 줄만(부분 인덱스) | 식별 줄은 `lookup_hash`를 쓰지 않는다. 가장 큰 종류의 줄이 인덱스에 들지 않는다 |
| `record_ref`의 미해결 인덱스 | "`state`가 해결이 아닌 줄의 부분 인덱스" | 부분 인덱스의 열은 (`src_no`, `dataset_no`, `key`) | 쓰임이 "미해결 레코드의 목록"이라 레코드의 키를 인덱스에서 바로 읽는다 |
| 계산용 테이블 | 문서의 목록 | `calc_dropped`(그 실행에서 충돌로 버린 키. 2.2절 규칙 3), `calc_object`(다시 계산할 객체의 목록)를 더했다 | 원형에서 규칙 3과 "영향받은 객체의 번호 순으로 페이지마다"에 필요했다. 문서 목록에 없는 둘이다 |
| `stage_object`, `stage_link`의 열 | 번호, `ref`, 타입, 이름, 문서, 내용 해시, 행 합, `first_key` | 타입 테이블과 `link`의 열 전부(`min_grade`, `source_refs`, `grade`, `basis` 등)와 `is_new` | 적용이 복사하므로 대상의 열을 그대로 가져야 한다 |
| `ingest_diff`의 열 | 순서, 청크 번호, 종류, 타입 번호, 번호, `ref`, 변화, 옛 해시, 새 해시 | `ctx_no`와 `sig_hash`를 더했다(키 줄만 쓴다). 번호의 열 이름은 `entity_no` | 키 줄이 묶는 키를 청크의 SQL이 `ingest_diff`에서 바로 넣으려면 그 키가 줄에 있어야 한다. 변경 항목의 이름과 맞췄다 |
| 키 줄의 다이제스트 | 줄은 (종류, 타입 번호, id, 변화, 옛 해시, 새 해시) | 키 줄의 새 해시를 키의 해시로 정했다 | 키 줄에는 해시가 없어서 그대로면 다이제스트가 묶이는 키의 값을 덮지 못한다 |
| 생성기가 바뀌었을 때 | ADR-0085 결정 6: 더하기만 따르고 생성기의 변경은 거절 | 알려진 더하기 단계는 따른다(결정 4) | 생성기 버전이 오르는 변경이 전부 KG를 다시 만들게 하면 안 된다. 더하기만 따르는 규칙은 그대로다 |
| `object_key`, `first_key`, 행 합의 열 | 설계가 "모자란 열이나 인덱스만" 더한다고 했다 | 더한 것이 없다 | ADR-0089가 이미 같은 열과 인덱스를 만들었다 |

## 이유

- 테이블이 먼저 있어야 뒤의 변경들(수집, 식별, 차이, 적용)이 같은 이름을 쓰고, 이 변경만 `Diff`와 매니페스트와 이미 있는 KG의 맞춤을 건드린다.
- 준비 테이블을 로그 있는 표로 두면 서버가 죽어도 계산 중간부터 이어 갈 수 있다. 비우기는 `TRUNCATE`라 로그가 늘지 않는다.
- 소스마다의 파티션은 소스의 끝나지 않은 실행이 하나라서 파티션을 통째로 비울 수 있게 한다.
- 다이제스트 줄을 고정 폭으로 한 곳에 적으면 SQL이 정규 인코딩을 다시 만들지 않고, 같은 줄에서 같은 값이 나오는 것을 테스트가 지킨다.
- 잠금 수를 재어 두면 소스를 붙이는 변경이 삭제의 길을 먼저 나누어야 하는 이유이 숫자로 남는다.

## 대안과 기각 사유

- **준비 테이블을 `UNLOGGED`로 둔다.** 책임자가 로그 있는 표로 정했다. 죽은 뒤 이어 가려면 내용이 남아야 한다.
- **파티션을 `CREATE TABLE ... PARTITION OF`로 만든다.** 다른 소스가 받는 동안 부모의 잠금을 기다린다.
- **소스마다가 아니라 `src_no`의 해시로 파티션한다.** 소스 하나를 `TRUNCATE`하거나 떼는 일이 안 된다.
- **`max_locks_per_transaction`을 올려서 소스가 많은 KG의 삭제를 한 트랜잭션에 넣는다.** 소스마다 약 68개가 더 들어 소스 수백 개에서는 설정이 터무니없이 커진다. 삭제를 나누는 것이 답이다.
- **`kind`를 `CHECK` 없는 글로 둔다.** 고정 목록이라는 것을 테이블이 지키게 하려고 `CHECK`를 둔다. 낱말이 더해지는 변경은 생성기 버전을 올리는 변경이다.
- **`generator_version` 불일치를 모두 거절하고 KG를 다시 만든다.** 이미 있는 KG가 더하기만으로 맞춰질 수 있다.

## 결과

- 새 KG는 처음부터 생성기 버전 3으로 만들고, 이 변경 앞에 만든 KG는 `AdjustStorageSchema`가 테이블을 더해 맞춘다. 공용의 새 마이그레이션 `0006`이 `sync_run`과 그 딸린 테이블을 만든다.
- 아직 이 테이블을 쓰는 코드는 없다. `adapter.pgingest`와 상태 기계, 격리, 소스를 붙이는 길은 뒤의 변경이다.
- **뒤의 변경이 해야 할 것**: 소스를 붙이는 길은 KG의 삭제가 소스를 먼저 하나씩 떼도록 하고(결정 8), 원본 레코드를 질의 스키마와 질의 역할에서 감추는 것(REQ-647)은 질의 역할이 생길 때 한다(지금은 역할이 없어서 테이블만 있다). `core.ingest`의 문제 종류 목록과 `pgschema.ProblemKinds`가 같은지는 `adapter.pgingest`의 테스트가 본다.
- 바뀌는 잠긴 테스트 한 건:

| 테스트 | 옛 | 새 |
|---|---|---|
| `TestControlReservesThePartitionPrefix` | 생성기 버전이 2 | 생성기 버전이 3. 파티션 접두어(`part_`)의 예약을 확인하는 앞부분은 그대로 |
