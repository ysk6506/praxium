---
title: ADR-0102 새 수집 흐름의 2단계(원본 차이)와 세 갈래 완전성
kind: decision
status: active
enforced_by:
  - GOARCH010
  - TLOCK001
  - backend/internal/pgingest/rdf_fates_test.go
  - backend/internal/pgingest/rdf_modes_test.go
  - backend/internal/pgingest/rdf_buckets_test.go
  - backend/internal/pgingest/rdf_receive_test.go
  - backend/internal/pgingest/rdf_parity_test.go
  - backend/internal/pgingest/rdf_plan_test.go
  - backend/internal/pgingest/rdf_faults_test.go
  - backend/internal/ingest/rawdiff_rdf_test.go
  - backend/internal/pgschema/rdf_generation_test.go
  - backend/internal/pgstore/rdf_generation_test.go
touches_protected:
  - harness/config/test-lock.json
  - docs/design-docs/adr/0095-ingestion-tables-in-the-schema-plan.md
  - docs/design-docs/adr/0098-receiving-step-and-run-ledger-in-adapter-pgingest.md
  - docs/product-specs/requirements-ingestion.md
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/ingestion-at-scale.md
  - docs/design-docs/ingestion-at-scale-identity.md
  - docs/product-specs/requirements-ingestion.md
amends:
  - adr: "0095"
    what: 결정 3과 4의 `record_wait`, `calc_record`, `record_problem`, `calc_problem`에 열을 더하거나 NULL을 허용하고, `stage_record`에 인덱스를 더하고, 생성기 버전을 6으로 올리는 것
  - adr: "0098"
    what: 수신이 데이터셋마다 남기는 수 줄에 완전성 세 갈래(`coverage`)를 더하는 것
amends_tests:
  - go:backend/internal/pgschema::TestControlReservesThePartitionPrefix
  - go:backend/internal/pgingest::TestPgiPuttingASourceToAKGAndTakingItOffHoldFewLocksAndDeletingAKGOfManySourcesFitsTheDefaultLockTable
  - go:backend/internal/pgschema::TestVmAKGOfGeneratorFourIsFollowedToFiveByMovingTheValuesAndBuildingTheNameIndexesAfterwards
  - go:backend/internal/pgschema::TestVmAKGOfGeneratorThreeIsFollowedThroughFourToFiveInOrder
test_change_reason: 생성기 버전이 5에서 6이 되어 버전 단언의 값이 바뀌고, 3에서 최신까지의 단계를 확인하던 테스트 둘은 이제 목표 버전(4 또는 5)을 명시해 같은 단언을 한다. 5에서 6의 단계는 새 테스트가 확인한다. 원본을 떼는 트랜잭션의 잠금 수는 받은 행의 인덱스 하나만큼 늘어 한도 100을 101(측정)로 넘으므로 한도를 110으로 올린다(소스마다 한정된 수라는 뜻은 그대로다). 단언은 줄지 않았다.
decision_hash: 46a1be6228cc469e
---
# ADR-0102 새 수집 흐름의 2단계(원본 차이)와 세 갈래 완전성

## 맥락

ADR-0086이 새 수집 흐름의 2단계(원본 차이)를 "받은 행과 이미 가진 원본 레코드를 SQL로 견주는 일"로 정했고, ADR-0098이 1단계(수신)와 실행 기록을 지었다. 이 결정은 2단계의 **코드**다. 3단계(식별)부터, 상태 기계(I-7), 정리(9단계), 완전성을 `seen_only`로 내는 어댑터는 하지 않는다. 옛 수집(`ingest.Runner`)은 그대로 두고, 새 길은 그 옆에 선택 능력으로 산다.

## 결정

1. **2단계는 `adapter.pgingest`의 SQL이고 포트는 `core.ingest`의 `RawDiffer`다.** 포트는 `RunLedger`와 같은 모양이다. 모든 쓰기가 임대의 시도 번호를 대고, 시도가 다르면 `CodeRunLeaseLost`로 거절하며 아무것도 바꾸지 않는다.

   | 메서드 | 하는 일 |
   |---|---|
   | `BeginRawDiff(lease, start)` | 계산 테이블(`calc_*`)을 비우고 그 실행이 주인이라고 적고(`calc_owner`), 실행 기록에서 데이터셋마다 수신이 말한 완전성을 읽고, 실행 기록을 첫 버킷에 놓는다. 시각(`start.Now`)과 버킷 크기는 실행 기록에 적어서, 이어받은 서버도 같은 시각으로 센다 |
   | `DiffRawBucket(lease, part)` | 실행 기록이 가리키는 버킷 하나를 한다(`Last`는 끝이 없는 마지막 버킷). 다른 버킷이거나 이미 끝났으면 `CodeRunStage`로 거절한다. 버킷의 끝 찾기, 버킷, 다음 시작의 기록과 `calc_part`의 증가, 임대 연장이 한 트랜잭션이다 |
   | `RawDiffPlace(no)` | 시작했는지, 끝났는지, 끝난 버킷의 수(다음 버킷). 시작했다고 말하려면 계산 테이블이 그 실행의 것이어야 한다(아래 결정 6의 `calc_owner`). 주인이 다르면 "시작 안 함"이다 |
   | `RawDiffReport(no)` | 끝난 버킷의 수를 데이터셋마다 합친다 |

   `RunRawDiff`가 위를 이어 돌린다. 시작하지 않았으면 시작하고, 실행 기록이 가리키는 버킷부터 하고, 버킷 사이에 컨텍스트를 본다. 계산 중이 아닌 실행은 `CodeRunStage`로 거절한다. 계산 테이블은 KG마다 하나여서, 확인을 기다리느라 KG의 관문을 놓은 사이 같은 KG의 다른 소스가 계산을 시작하면 비워진다. 그래서 되돌아온 실행은 주인이 아니고 "시작 안 함"으로 보고되어, `RunRawDiff`가 2단계를 처음부터 다시 한다(시각은 호출이 준 `start.Now`, 곧 실행 기록의 시각이다).

2. **버킷은 (데이터셋 번호, 키)의 범위 [시작, 끝)이다.** 처음에 해시의 앞자리로 나누었다가 바꾸었다. 해시 조건은 인덱스로 읽을 수 없어서 버킷마다 준비 파티션을 처음부터 훑게 되므로 비용이 (버킷 수 × 행 수)에 비례하고, 소스 하나에 수천만 행이면 수백억 행을 읽는다. 키 범위는 이렇게 한다.
   - 다음 경계는 인덱스만 읽어 정한다. 받은 행(`stage_record`), 원본 레코드(`raw_record`), 기다림(`record_wait`) 각각에서 시작부터 `OFFSET <버킷 크기> LIMIT 1`로 키를 읽고 셋 중 가장 작은 키가 끝이다. 어느 테이블도 그만큼 행이 없으면 끝이 없는 마지막 버킷이다. 첫 버킷의 시작과 마지막 버킷의 끝은 열려 있어서(데이터셋 번호의 앞과 뒤) 새 키도 빠지지 않는다. 같은 키가 버킷 크기보다 많은 행을 가져서 끝이 시작과 같아지면 그 키 하나만으로 버킷을 만든다.
   - 버킷의 문장은 `raw_record`(기본키), `stage_record`, `record_wait`(기본키), `record_problem`을 (데이터셋 번호, 키)의 범위 조건으로만 읽는다. 전체 비용은 행 수에 비례한다(`EXPLAIN`으로 범위 스캔임을 테스트한다. 소스의 크기를 3배로 해도 버킷이 읽는 행은 같다).
   - 경계와 진행은 실행 기록(`counts.raw_diff.lo`, `done`, `calc_part`)에 적는다. 버킷이 만든 수는 버킷마다 줄을 더하지 않고 데이터셋별 누적 합(`counts.raw_diff.totals`) 한 줄씩만 둔다(버킷이 3만 개여도 실행 기록이 커지지 않는다). 경계 찾기, 버킷, 기록이 한 트랜잭션이고 시도 번호가 조건이라, 이어받은 서버는 같은 시작에서 같은 끝을 찾는다(버킷 사이에 테이블이 바뀌지 않으므로).
   - 버킷 크기는 `BucketRows`(기본 `DefaultRawDiffBucketRows` = 100,000)다. 한 버킷은 임대(60초)의 3분의 1인 20초 안이어야 하고 기본값은 측정(아래)으로 정한 시작 값이다. 버킷이 만드는 것은 버킷 크기와 상관없다(테스트가 크기 5, 2, 1로 확인한다).
   - 이유와 값: 인덱스는 기존 것을 쓴다(`raw_record`와 `record_wait`의 기본키). `stage_record`에는 없어서 생성기 버전 6의 단계로 `stage_record_key` (데이터셋 번호, 키, 페이지, 행 번호)를 더한다. 저장 공간은 받은 행마다 인덱스 항목 하나(키 글자와 숫자 셋)가 늘고, 수신은 소스마다 `TRUNCATE`하고 다시 받으므로 인덱스도 함께 비워진다. 수신의 `COPY`는 인덱스를 갱신하는 만큼 느려진다(이 변경에서는 측정하지 않았다).

3. **3단계가 읽는 출력 슬롯**은 `calc_record`, `calc_problem`, `calc_wait`다. 값에 부호나 특별한 문자열을 싣지 않는다.

   | 운명 | `calc_record` | 그 밖 |
   |---|---|---|
   | 새로 옴 | `why` = `CalcWhyNew`(3). `fields`, `raw_hash`, `decl_rev`는 받은 행의 것, `opened_seq`는 이 실행의 번호, `fate`는 쓰임 또는 걸러짐, `page`, `row_no`는 이긴 행의 자리 | |
   | 바뀜 | `why` = `CalcWhyChanged`(0). 위와 같다 | |
   | 돌아옴(안 보이던 키가 다시 보임, 걸러지던 키가 걸러지지 않음) | `why` = `CalcWhyReturned`(4). 값은 원본 레코드의 것 | `calc_wait`에서 그 줄이 빠진다 |
   | 안 보임의 기다림이 끝남 | `why` = `CalcWhyClosed`(5). 값은 비어 있다(`fields` NULL) | `calc_wait`에 `kind`=안 보임, `expired`=참 |
   | 걸러짐의 기다림이 끝남(처음 한 번) | `why` = `CalcWhyReleased`(6). 값은 원본 레코드의 것. 이번에 보이지 않는 키도 같다(아래) | `calc_wait`에 `kind`=걸러짐, `expired`=참 |
   | 반복된 키가 처음 생기거나 끝남 | `why` = `CalcWhyDuplicates`(7) | |
   | 배치 안의 중복(이긴 행 하나) | 위 이유가 있으면 그 줄이 이긴 행의 자리를 가진다 | `calc_problem`에 키마다 `duplicate_key` 한 줄. 정의 번호 `def_no`는 NULL |
   | 그대로 | 줄 없음 | |
   | 안 보임(아직 기다리는 중) | 줄 없음 | `calc_wait`에 `kind`=안 보임, 센 횟수와 처음 시각 |
   | 안 보이지만 원본 레코드가 열려 있고 걸러짐의 기다림이 있음 | 시한이 차면 `CalcWhyReleased`, 아니면 줄 없음 | `calc_wait`에 `kind`=걸러짐, 횟수 + 1, 처음 시각 그대로. 안 보임의 기다림과 함께 간다(한 레코드가 둘 다일 수 있다) |

   **걸러짐의 기다림은 원본 레코드가 열려 있는 동안 센다.** 옛 구현이 열린 원본 레코드 전체에 걸러짐을 세는 것(`materialize`가 `state.Open`을 읽고 `countFiltered`가 센다)과 같다. 완전한 데이터셋에서 이번에 안 보이지만 원본 레코드가 남아 있고 걸러짐의 기다림이 있는 키도 그 기다림이 한 번 더 간다. 걸러졌다가, 안 보였다가, 다시 걸러지기를 번갈아 하는 키도 옛 구현과 같은 때에 풀림을 낸다. 이 실행이 안 보임의 기다림으로 레코드를 닫으면 걸러짐은 세지 않고 줄도 옮기지 않는다. 삭제 규칙이 `none`인 데이터셋은 안 보임을 세지 않지만 걸러짐은 세고 시한이 차지 않는다.

   **`why`는 하나뿐이어서 우선순위가 있다**: 새로 옴 > 바뀜 > 돌아옴 > 풀림 > 반복. 한 키가 여럿에 해당하면 가장 앞의 것이 `why`가 된다. 그래서 `why`만 읽으면 다른 이유를 놓친다. (1) 걸러진 레코드를 잡아 둘지는 `why`가 아니라 `calc_wait.expired`로 정한다(바뀌면서 풀린 레코드는 `why`가 바뀜이고 `expired`가 참이다). 풀림은 `calc_wait`가 처음 한 번만 말해서 다음 실행에는 `expired`만 남고 `why`는 없다. (2) 돌아옴의 수는 `why`가 아니라 보고의 수(`RawDiffCounts.Returned`)다. `Returned`와 풀림(`Released`)은 `why`가 다른 이유로 가려도 센다.

   새 횟수(`runs`)와 처음 시각(`first_at`, 초 단위로 자른다)은 여기서 센다. 정리는 옮기기만 한다. 기다림의 시간 비교는 실행 시각(`start.Now`)으로 하고 데이터베이스의 시계는 쓰지 않는다.

4. **9단계(정리)가 지킬 약속.** `why`가 새로 옴과 바뀜인 `calc_record`는 `raw_record`에 덮어쓴다(`INSERT ... ON CONFLICT DO UPDATE`). 닫힘인 줄은 `raw_record`에서 지운다. `record_wait`는 `calc_wait`로 바꿔 놓되, 안 보임이면서 `expired`인 줄은 옮기지 않는다(그 레코드는 이미 닫혔다). 걸러짐의 `expired`는 옮긴다(풀린 뒤 다시 풀지 않으려는 것이다). **반복된 키의 문제(`duplicate_key`)는 2단계의 판단만 믿고 따로 옮긴다**: `calc_problem`의 `duplicate_key`는 `record_problem`에 넣고, 이번에 반복이 끝난 키의 옛 `duplicate_key` 줄만 지운다. "반복이 끝남"은 2단계가 자기 이유(`why`가 바뀜, 새로 옴, 돌아옴, 닫힘, 풀림, 반복)로 `calc_record`에 낸 키 가운데 `calc_problem`에 `duplicate_key`가 없는 것이다(한 키의 이유가 우선순위로 가려져도 그 키의 줄은 같은 실행에서 정리된다). 3단계 이후가 다른 이유(`CalcWhyUnresolved`, `CalcWhyExpanded`)로 `calc_record`에 더한 레코드는 반복 문제를 건드리지 않는다. 이 약속은 테스트의 `settle`이 같은 순서로 하고 있고(반복 → 그대로 반복 → 반복 끝을 실행 셋으로 확인한다) 9단계의 코드가 지킨다.

5. **완전성은 세 갈래다**: 완전(`complete`), 본 것만(`seen_only`), 불완전(`incomplete`). 코드와 실행 기록의 값이 이 표기 그대로다.
   - `core.ingest`의 `Completeness`에 `SeenOnly`를 더하고 `Coverage`(세 값)를 낸다. 기존 코드와 잠긴 테스트가 읽는 `Complete`는 그대로다. `Complete`와 `SeenOnly`가 함께 참인 값은 서로 맞지 않아서 수신이 거절한다(`Completeness.Check`, `CodeInvalidValue`). 그 값에 `Coverage`를 물으면 더 위험한 쪽(안 보임을 세고 닫음)이 아니라 불완전로 읽는다.
   - 어댑터가 마지막 페이지(`End`)에서 내고, 수신(`Intake`)이 데이터셋마다 실행 기록의 수(`counts.receive.datasets[].coverage`)에 적는다. 읽지 못한 부분이 있는 데이터셋은 불완전이다. 이 값이 없는 옛 기록은 `complete` 플래그로 읽고, 값이 있는데 세 값이 아니면 불완전으로 읽는다(모르는 값으로 안 보임을 세지 않는다).
   - 2단계에서 완전한 데이터셋만 안 보임을 센다. 본 것만이면 보인 키는 평소대로 새로 옴, 바뀜, 돌아옴을 가리고 안 보임은 세지 않으며 이미 있는 기다림은 그대로 둔다. 불완전이거나 수신이 주지 않은 데이터셋은 통째로 뺀다(바뀐 행도 반영하지 않고 기다림도 그대로다). 삭제 규칙이 `none`인 데이터셋은 안 보임을 세지 않는다(옛 구현과 같다). 걸러짐은 세지만 시한이 차지 않는다. 본 것만을 내는 어댑터는 이 변경에 없다.

6. **스키마는 생성기 버전 6으로 더한다.** 버전 5는 ADR-0101이 썼고(값을 열로 옮기고 이름 인덱스를 만든다), 이 변경의 단계는 그 위에 더한다. 5의 단계는 그대로 두고 6의 단계(`rawDiffSteps`)만 `generations.go`에 더했다. 모두 `ADD COLUMN IF NOT EXISTS`와 `DROP NOT NULL`이라 데이터를 잃지 않는다.
   - `record_wait.expired`(`boolean NOT NULL DEFAULT false`)와 `calc_wait.expired`: 기다림이 끝났다는 표시를 따로 둔다. 걸러짐이 풀린 뒤 다음 실행이 또 풀지 않게 하는 상태다.
   - `calc_record.page`, `calc_record.row_no`: 이긴 행의 자리.
   - `record_problem.def_no`, `calc_problem.def_no`를 NULL 허용으로: 반복된 키의 문제는 정의에 속하지 않는다.
   - `stage_record_key`: 받은 행의 인덱스(위 결정 2). `CREATE INDEX IF NOT EXISTS`다.
   - `calc_owner`(`run_no bigint`, 한 줄): 계산 테이블이 어느 실행의 것인지. `BeginRawDiff`가 계산 테이블을 비우면서 적고, `RawDiffPlace`가 "끝남"이라 말하기 전에 확인한다. 계산 테이블은 KG마다 하나라서 실행의 번호 없이는 되돌아온 실행이 남의 줄을 읽는지 알 수 없다. 다른 계산 테이블과 함께 비워지고, 이미 있는 KG에는 6의 단계가 만든다.
   - 이름 상수: `CalcWhyNew`부터 `CalcWhyDuplicates`, `CalcJobRawDiff`(1).
   - 버전 5나 4인 KG는 조정(adjust) 때 위 단계를 받는다(`pgstore`의 테스트가 확인한다).

7. **옛 의미를 지킨다.** 옛 `ingest.Runner`(`rawrec.go`, `filtered.go`, 배치의 중복 제거)와 같은 사례에서 같은 원본 레코드, 같은 기다림(횟수와 처음 시각), 같은 수, 같은 살아 있는 객체를 내는지를 테스트(`rdf_parity_test.go`)가 확인한다. 옛 구현을 `ManualClock`과 주입한 `now`로 돌려 같은 입력을 나란히 견준다. 원본 레코드는 키뿐 아니라 내용(필드, 해시), 운명, 연 실행의 번호(`opened_seq`)까지 견준다. 표 테스트는 25단계 × 데이터셋 둘이고 한 버킷과 버킷 크기 1로 두 번 돌린다. 사례는 반복된 키, 사라졌다 돌아온 키, 계속 사라진 키, 걸러짐에서 풀림과 다시 보임, 걸러졌다 안 보였다 다시 걸러짐을 되풀이하는 키(걸러진 레코드가 안 보이는 동안에도 기다림이 간다), 주지 않은 데이터셋, 잘린 데이터셋, 삭제 규칙 `none`이다. 무작위 입력(씨앗 셋, 30단계, 버킷 크기 1, 2, 기본)도 같은 것을 견준다. 다른 점은 아래 표에 이유와 함께 있다.

   | 다른 점 | 옛 | 새 | 이유 |
   |---|---|---|---|
   | 반복된 키의 문제 | 중복 행마다 정의 번호가 있는 문제 | 키마다 `duplicate_key` 한 줄, `def_no` NULL, 자리 없음 | 문제는 정의와 상관없는 키의 사실이다. 줄이 키마다 하나여서 버킷이 나뉘어도 같다 |
   | 걸러짐 여부가 뒤집힌 행(해시는 같음) | 같은 행(그대로)으로 세고 원본 레코드를 그대로 둔다 | 수신(`Sink`)이 걸러짐 여부가 뒤집힌 행을 바뀜으로 내고, 2단계는 그 `change_kind`를 그대로 쓴다. 원본 레코드의 운명이 바뀌고 `opened_seq`가 이 실행이 된다 | 비교의 근거(받은 행의 해시와 걸러짐 여부)를 수신이 이미 갖고 있다. 원본 레코드의 내용, 기다림, 살아 있는 객체는 같고 수정과 그대로의 수, `opened_seq`만 다르다(같은 행을 매핑만 바꿔 되풀이하는 테스트가 견주어 확인한다) |
   | 본 것만 | 없음 | 위 5의 규칙 | 새 입력이라 옛 짝이 없다. 자체 불변식으로 테스트한다 |

8. **측정(보고만, 한 번).** 레코드 100만, 바뀐 것 10%, 안 보이는 것 1%에서 2단계의 시간, 버킷 수, 가장 긴 버킷은 아래 "측정" 절에 적는다. 이 측정은 테스트로 남기지 않는다.

## 측정

벤치마크 `BenchmarkRdfRawDiff`(`go test ./internal/pgingest -run '^$' -bench RdfRawDiff -benchtime 1x`)로 한 번 쟀다. 데이터는 DB 안에서 `generate_series`로 만든다. 소스가 레코드 n개를 가지고, 받은 행은 그중 99%이고(1%는 안 보임), 10%가 바뀌었다. 기본 버킷 크기(100,000)다. 2코어 컨테이너의 PostgreSQL 18, 다른 부하가 없는 때의 값이고 어림으로만 읽는다.

| 레코드 | 2단계 전체 | 버킷 수 | 가장 긴 버킷 |
|---|---|---|---|
| 100,000 | 0.51초 | 1 | 0.50초 |
| 1,000,000 | 4.5초 | 10 | 0.70초 |

가장 긴 버킷도 한도(20초)의 20분의 1이어서 기본 크기를 그대로 둔다. 처음 짠 문장은 같은 조인을 중첩 루프로 계획해서 버킷 크기의 제곱에 비례했다(5,000행 버킷이 0.6초). 버킷의 트랜잭션에서 중첩 루프를 끄고(`SET LOCAL enable_nestloop = off`) 해시 조인으로 바꾸고(원본 레코드를 찾는 조인도 버킷의 범위로 좁힌다) 고쳤고, 계획의 꼴(범위 스캔, 조인이 버린 쌍의 수)을 테스트가 확인한다.

## 알려진 한계

- 받은 행의 인덱스 때문에 수신의 쓰기가 늘어난다. 측정은 2단계만 했다.
- 5에서 6으로 조정할 때 `stage_record_key`를 트랜잭션 안에서 만들어, 모든 소스의 받은 행(지난 수신이 끝나지 않고 남은 행 포함)을 색인하고 그동안 그 KG의 수신이 막힌다. 큰 KG는 조정이 길어진다. 진행 중인 실행이 없을 때 조정 전에 받은 행을 비우는 방법은 쓰지 않았다. 단계는 스키마 이름만 알고 실행의 상태(공유 테이블의 실행 기록)를 읽지 못하며, 확인한 뒤 비우기 전에 실행이 수신을 시작하면 그 행을 잃는다. 한 번의 조정에만 드는 비용이라 한계로 둔다.
- 버킷 크기의 기본값은 시작 값이다. 한 버킷이 20초를 넘으면 `BucketRows`로 줄인다(설정이 아니라 실행의 시작 값이다).

## 결과

- 3단계는 `calc_record`, `calc_problem`, `calc_wait`만 읽는다. 원본 레코드와 `record_wait`는 9단계 전까지 바뀌지 않는다.
- 상태 기계(I-7)는 `RunRawDiff`를 계산 단계의 첫 일로 부르면 된다. 불러도 되는 때: 실행 기록의 `calc_job`이 0(아직 시작 전)이거나 2단계(1)일 때, 그리고 확인을 기다리다 KG의 관문을 다시 얻어 계산으로 되돌아온 때다. 되돌아온 때는 계산 테이블의 주인을 확인해서 다른 실행이 비웠으면 2단계부터 다시 하고, 주인이면 끝난 비교를 다시 하지 않고 보고만 한다. 3단계 이후의 일이 시작된 실행(`calc_job`이 2 이상)에는 부르지 않는다. 부르면 "시작 안 함"으로 보고되어 계산 테이블을 비우고 처음부터 다시 한다.
