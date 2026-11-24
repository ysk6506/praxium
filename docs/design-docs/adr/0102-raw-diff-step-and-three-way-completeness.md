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
  - go:backend/internal/pgschema::TestIngestHeldDiffFromGeneratorThreeAddsTheColumnHeldToTheThreeTablesOfTheRowsReceivedAndNothingElse
  - go:backend/internal/pgingest::TestPgiPuttingASourceToAKGAndTakingItOffHoldFewLocksAndDeletingAKGOfManySourcesFitsTheDefaultLockTable
test_change_reason: 생성기 버전이 4에서 6이 되어 버전 단언의 값이 바뀌고, 3에서 최신까지의 단계를 확인하던 테스트는 이제 3에서 4의 단계를 명시해 같은 단언을 한다. 5에서 6의 단계는 새 테스트가 확인한다. 원본을 떼는 트랜잭션의 잠금 수는 받은 행의 인덱스 하나만큼 늘어 한도 100을 101로 넘으므로 한도를 110으로 올린다(소스마다 한정된 수라는 뜻은 그대로다). 단언은 줄지 않았다.
decision_hash: b787134d2c8b1507
---
# ADR-0102 새 수집 흐름의 2단계(원본 차이)와 세 갈래 완전성

## 맥락

ADR-0086이 새 수집 흐름의 2단계(원본 차이)를 "받은 행과 이미 가진 원본 레코드를 SQL로 견주는 일"로 정했고, ADR-0098이 1단계(수신)와 실행 기록을 지었다. 이 결정은 2단계의 **코드**다. 3단계(식별)부터, 상태 기계(I-7), 정리(9단계), 완전성을 `seen-only`로 내는 어댑터는 하지 않는다. 옛 수집(`ingest.Runner`)은 그대로 두고, 새 길은 그 옆에 선택 능력으로 산다.

## 결정

1. **2단계는 `adapter.pgingest`의 SQL이고 포트는 `core.ingest`의 `RawDiffer`다.** 포트는 `RunLedger`와 같은 모양이다. 모든 쓰기가 임대의 시도 번호를 대고, 시도가 다르면 `CodeRunLeaseLost`로 거절하며 아무것도 바꾸지 않는다.

   | 메서드 | 하는 일 |
   |---|---|
   | `BeginRawDiff(lease, start)` | 계산 테이블(`calc_*`)을 비우고, 실행 기록에서 데이터셋마다 수신이 말한 완전성을 읽고, 실행 기록을 첫 버킷에 놓는다. 시각(`start.Now`)과 버킷 크기는 실행 기록에 적어서, 이어받은 서버도 같은 시각으로 센다 |
   | `DiffRawBucket(lease, part)` | 실행 기록이 가리키는 버킷 하나를 한다(`Last`는 끝이 없는 마지막 버킷). 다른 버킷이거나 이미 끝났으면 `CodeRunStage`로 거절한다. 버킷의 끝 찾기, 버킷, 다음 시작의 기록과 `calc_part`의 증가, 임대 연장이 한 트랜잭션이다 |
   | `RawDiffPlace(no)` | 시작했는지, 끝났는지, 끝난 버킷의 수(다음 버킷) |
   | `RawDiffReport(no)` | 끝난 버킷의 수를 데이터셋마다 합친다 |

   `RunRawDiff`가 위를 이어 돌린다. 시작하지 않았으면 시작하고, 실행 기록이 가리키는 버킷부터 하고, 버킷 사이에 컨텍스트를 본다. 계산 중이 아닌 실행은 `CodeRunStage`로 거절한다.

2. **버킷은 (데이터셋 번호, 키)의 범위 [시작, 끝)이다.** 처음에 해시의 앞자리로 나누었다가 바꾸었다. 해시 조건은 인덱스로 읽을 수 없어서 버킷마다 준비 파티션을 처음부터 훑게 되므로 비용이 (버킷 수 × 행 수)에 비례하고, 소스 하나에 수천만 행이면 수백억 행을 읽는다. 키 범위는 이렇게 한다.
   - 다음 경계는 인덱스만 읽어 정한다. 받은 행(`stage_record`), 원본 레코드(`raw_record`), 기다림(`record_wait`) 각각에서 시작부터 `OFFSET <버킷 크기> LIMIT 1`로 키를 읽고 셋 중 가장 작은 키가 끝이다. 어느 테이블도 그만큼 행이 없으면 끝이 없는 마지막 버킷이다. 첫 버킷의 시작과 마지막 버킷의 끝은 열려 있어서(데이터셋 번호의 앞과 뒤) 새 키도 빠지지 않는다. 같은 키가 버킷 크기보다 많은 행을 가져서 끝이 시작과 같아지면 그 키 하나만으로 버킷을 만든다.
   - 버킷의 문장은 `raw_record`(기본키), `stage_record`, `record_wait`(기본키), `record_problem`을 (데이터셋 번호, 키)의 범위 조건으로만 읽는다. 전체 비용은 행 수에 비례한다(`EXPLAIN`으로 범위 스캔임을 테스트한다. 소스의 크기를 3배로 해도 버킷이 읽는 행은 같다).
   - 경계와 진행은 실행 기록(`counts.raw_diff.lo`, `done`, `calc_part`)에 적는다. 경계 찾기, 버킷, 기록이 한 트랜잭션이고 시도 번호가 조건이라, 이어받은 서버는 같은 시작에서 같은 끝을 찾는다(버킷 사이에 테이블이 바뀌지 않으므로).
   - 버킷 크기는 `BucketRows`(기본 `DefaultRawDiffBucketRows` = 100,000)다. 한 버킷은 임대(60초)의 3분의 1인 20초 안이어야 하고 기본값은 측정(아래)으로 정한 시작 값이다. 버킷이 만드는 것은 버킷 크기와 상관없다(테스트가 크기 5, 2, 1로 확인한다).
   - 이유와 값: 인덱스는 기존 것을 쓴다(`raw_record`와 `record_wait`의 기본키). `stage_record`에는 없어서 생성기 버전 6의 단계로 `stage_record_key` (데이터셋 번호, 키, 페이지, 행 번호)를 더한다. 저장 공간은 받은 행마다 인덱스 항목 하나(키 글자와 숫자 셋)가 늘고, 수신은 소스마다 `TRUNCATE`하고 다시 받으므로 인덱스도 함께 비워진다. 수신의 `COPY`는 인덱스를 갱신하는 만큼 느려진다(이 변경에서는 측정하지 않았다).

3. **3단계가 읽는 출력 슬롯**은 `calc_record`, `calc_problem`, `calc_wait`다. 값에 부호나 특별한 문자열을 싣지 않는다.

   | 운명 | `calc_record` | 그 밖 |
   |---|---|---|
   | 새로 옴 | `why` = `CalcWhyNew`(3). `fields`, `raw_hash`, `decl_rev`는 받은 행의 것, `opened_seq`는 이 실행의 번호, `fate`는 쓰임 또는 걸러짐, `page`, `row_no`는 이긴 행의 자리 | |
   | 바뀜 | `why` = `CalcWhyChanged`(0). 위와 같다 | |
   | 돌아옴(안 보이던 키가 다시 보임, 걸러지던 키가 걸러지지 않음) | `why` = `CalcWhyReturned`(4). 값은 원본 레코드의 것 | `calc_wait`에서 그 줄이 빠진다 |
   | 안 보임의 기다림이 끝남 | `why` = `CalcWhyClosed`(5). 값은 비어 있다(`fields` NULL) | `calc_wait`에 `kind`=안 보임, `expired`=참 |
   | 걸러짐의 기다림이 끝남(처음 한 번) | `why` = `CalcWhyReleased`(6). 값은 원본 레코드의 것 | `calc_wait`에 `kind`=걸러짐, `expired`=참 |
   | 반복된 키가 처음 생기거나 끝남 | `why` = `CalcWhyDuplicates`(7) | |
   | 배치 안의 중복(이긴 행 하나) | 위 이유가 있으면 그 줄이 이긴 행의 자리를 가진다 | `calc_problem`에 키마다 `duplicate_key` 한 줄. 정의 번호 `def_no`는 NULL |
   | 그대로 | 줄 없음 | |
   | 안 보임(아직 기다리는 중) | 줄 없음 | `calc_wait`에 `kind`=안 보임, 센 횟수와 처음 시각 |

   새 횟수(`runs`)와 처음 시각(`first_at`, 초 단위로 자른다)은 여기서 센다. 정리는 옮기기만 한다. 기다림의 시간 비교는 실행 시각(`start.Now`)으로 하고 데이터베이스의 시계는 쓰지 않는다.

4. **9단계(정리)가 지킬 약속.** `why`가 새로 옴과 바뀜인 `calc_record`는 `raw_record`에 덮어쓴다(`INSERT ... ON CONFLICT DO UPDATE`). 닫힘인 줄은 `raw_record`에서 지운다. `record_wait`는 `calc_wait`로 바꿔 놓되, 안 보임이면서 `expired`인 줄은 옮기지 않는다(그 레코드는 이미 닫혔다). 걸러짐의 `expired`는 옮긴다(풀린 뒤 다시 풀지 않으려는 것이다). 이 약속은 테스트의 `settle`이 같은 순서로 하고 있고 9단계의 코드가 지킨다.

5. **완전성은 세 갈래다**: 완전(`complete`), 본 것만(`seen-only`), 불완전(`incomplete`).
   - `core.ingest`의 `Completeness`에 `SeenOnly`를 더하고 `Coverage`(세 값)를 낸다. `Complete`가 참이면 완전이 이긴다. 기존 코드와 잠긴 테스트가 읽는 `Complete`는 그대로다.
   - 어댑터가 마지막 페이지(`End`)에서 내고, 수신(`Intake`)이 데이터셋마다 실행 기록의 수(`counts.receive.datasets[].coverage`)에 적는다. 읽지 못한 부분이 있는 데이터셋은 불완전이다. 이 값이 없는 옛 기록은 `complete` 플래그로 읽는다.
   - 2단계에서 완전한 데이터셋만 안 보임을 센다. 본 것만이면 보인 키는 평소대로 새로 옴, 바뀜, 돌아옴을 가리고 안 보임은 세지 않으며 이미 있는 기다림은 그대로 둔다. 불완전이거나 수신이 주지 않은 데이터셋은 통째로 뺀다(바뀐 행도 반영하지 않고 기다림도 그대로다). 삭제 규칙이 `none`인 데이터셋은 안 보임을 세기만 하고 끝내지 않는다. 본 것만을 내는 어댑터는 이 변경에 없다.

6. **스키마는 생성기 버전 6으로 더한다.** 버전 5는 다른 변경(ADR-0101)이 쓴다. 이 빌드에는 그 단계가 아직 없어서 `generations.go`에 아무것도 더하지 않는 단계(`noSteps`)를 두고, 그 변경을 합칠 때 그 단계로 바꾼다. 모두 `ADD COLUMN IF NOT EXISTS`와 `DROP NOT NULL`이라 데이터를 잃지 않는다.
   - `record_wait.expired`(`boolean NOT NULL DEFAULT false`)와 `calc_wait.expired`: 기다림이 끝났다는 표시를 따로 둔다. 걸러짐이 풀린 뒤 다음 실행이 또 풀지 않게 하는 상태다.
   - `calc_record.page`, `calc_record.row_no`: 이긴 행의 자리.
   - `record_problem.def_no`, `calc_problem.def_no`를 NULL 허용으로: 반복된 키의 문제는 정의에 속하지 않는다.
   - `stage_record_key`: 받은 행의 인덱스(위 결정 2). `CREATE INDEX IF NOT EXISTS`다.
   - 이름 상수: `CalcWhyNew`부터 `CalcWhyDuplicates`, `CalcJobRawDiff`(1).
   - 버전 5나 4인 KG는 조정(adjust) 때 위 단계를 받는다(`pgstore`의 테스트가 확인한다).

7. **옛 의미를 지킨다.** 옛 `ingest.Runner`(`rawrec.go`, `filtered.go`, 배치의 중복 제거)와 같은 사례에서 같은 원본 레코드, 같은 기다림(횟수와 처음 시각), 같은 수, 같은 살아 있는 객체를 내는지를 표 테스트(`rdf_parity_test.go`)가 확인한다. 옛 구현을 `ManualClock`과 주입한 `now`로 돌려 17단계 × 데이터셋 둘의 사례를 견주었다. 사례는 반복된 키, 사라졌다 돌아온 키, 계속 사라진 키, 걸러짐에서 풀림과 다시 보임, 주지 않은 데이터셋, 잘린 데이터셋, 삭제 규칙 `none`이다. 다른 점은 아래 표에 이유와 함께 있다.

   | 다른 점 | 옛 | 새 | 이유 |
   |---|---|---|---|
   | 반복된 키의 문제 | 중복 행마다 정의 번호가 있는 문제 | 키마다 `duplicate_key` 한 줄, `def_no` NULL, 자리 없음 | 문제는 정의와 상관없는 키의 사실이다. 줄이 키마다 하나여서 버킷이 나뉘어도 같다 |
   | 걸러짐 여부가 뒤집힌 행 | 원본 레코드의 변경 종류를 2단계가 정함 | 수신(`Sink`)이 걸러짐 여부가 뒤집힌 행을 바뀜으로 내고, 2단계는 그 `change_kind`를 그대로 쓴다 | 비교의 근거(받은 행의 해시와 걸러짐 여부)를 수신이 이미 갖고 있다. 객체의 결과는 같다(견주어 확인) |
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
- 버킷 크기의 기본값은 시작 값이다. 한 버킷이 20초를 넘으면 `BucketRows`로 줄인다(설정이 아니라 실행의 시작 값이다).

## 결과

- 3단계는 `calc_record`, `calc_problem`, `calc_wait`만 읽는다. 원본 레코드와 `record_wait`는 9단계 전까지 바뀌지 않는다.
- 상태 기계(I-7)는 `RunRawDiff`를 계산 단계의 첫 일로 부르면 된다.
