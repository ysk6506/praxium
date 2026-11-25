---
title: ADR-0108 새 수집 흐름의 3단계(영향 집합의 출발과 식별)
kind: decision
status: active
enforced_by:
  - GOARCH010
  - TLOCK001
  - backend/internal/pgingest/identify_test.go
  - backend/internal/pgingest/idn_alias_test.go
  - backend/internal/pgingest/idn_claims_test.go
  - backend/internal/pgingest/idn_resume_test.go
  - backend/internal/pgingest/idn_unresolved_test.go
  - backend/internal/pgingest/idn_owner_test.go
  - backend/internal/pgingest/idn_parity_test.go
  - backend/internal/pgingest/idn_parity_more_test.go
  - backend/internal/pgingest/idn_chain_test.go
  - backend/internal/pgingest/idn_fates_test.go
  - backend/internal/pgingest/idn_sources_test.go
  - backend/internal/pgingest/idn_begin_test.go
  - backend/internal/pgstore/ident_generation_test.go
  - backend/internal/pgingest/idn_plan_test.go
  - backend/internal/ingest/idn_name_test.go
  - backend/internal/pgschema/ident_generation_test.go
touches_protected:
  - harness/config/test-lock.json
  - docs/design-docs/adr/0095-ingestion-tables-in-the-schema-plan.md
  - docs/design-docs/adr/0102-raw-diff-step-and-three-way-completeness.md
  - docs/product-specs/requirements-ingestion.md
updates_docs:
  - docs/design-docs/ingestion-at-scale.md
  - docs/design-docs/ingestion-at-scale-identity.md
  - docs/product-specs/requirements-ingestion.md
amends:
  - adr: "0095"
    what: 결정 3과 4의 `calc_key`, `calc_end`, `calc_claim`에 `held` 열을, `calc_bind`에 `feed_no` 열을 더하고, 작업 테이블 다섯 개와 인덱스를 더하고, 생성기 버전을 7로 올리는 것
amends_tests:
  - go:backend/internal/pgschema::TestControlReservesThePartitionPrefix
  - go:backend/internal/pgschema::TestRdfDiffFromGeneratorFiveAddsTheWaitsExpiredTheRowOfARecordTheProblemsThatHaveNoDefinitionTheOwnerOfTheComputationAndTheIndexOfTheRowsReceivedAndNothingElse
  - go:backend/internal/pgschema::TestRdfDiffFromGeneratorThreeToTheNewestIsTheStepsOfFourAndThenSix
test_change_reason: 생성기 버전이 6에서 7이 되어, 버전 단언의 값이 바뀌고(6에서 7), 5에서 최신까지와 3에서 최신까지의 단계를 확인하던 두 테스트는 목표 버전(6)을 명시해 같은 단언을 한다. 6에서 7의 단계는 새 테스트(`ident_generation_test.go`)가 확인한다. 단언은 줄지 않았다.
decision_hash: b3be35b059953cc3
---
# ADR-0108 새 수집 흐름의 3단계(영향 집합의 출발과 식별)

## 맥락

ADR-0086이 새 수집 흐름의 3단계를 "출발 레코드를 `calc_record`에 넣고, 키를 찾고 묶어서 레코드마다 객체를 정하는 일"로 정했고, ADR-0102가 2단계의 출력 슬롯을 정했다. 이 결정은 3단계의 **코드**다. 4단계(다시 계산)부터, 상태 기계와 정리(I-7), 여러 소스의 순위(I-9)는 하지 않는다. 옛 수집(`ingest.Runner`)은 그대로 둔다.

## 결정

1. **포트는 `core.ingest`의 `Identifier`이고 구현은 `adapter.pgingest`의 SQL이다.** `RawDiffer`와 같은 모양이다. 모든 쓰기가 임대의 시도 번호를 대고, 시도가 다르면 `CodeRunLeaseLost`로 거절하며 아무것도 바꾸지 않는다.

   | 메서드 | 하는 일 |
   |---|---|
   | `BeginIdentify(lease, start)` | 계산 테이블의 주인이 이 실행인지 확인하고(`calc_owner`), 실행 기록의 `calc_job`을 3단계(2)로 바꾸고 자리를 처음에 놓는다. 계산 테이블은 비우지 않는다(2단계가 채운 것을 읽는다). 이미 시작한 실행(`calc_job`이 3단계)과 2단계(비교)가 끝나지 않은 실행은 `CodeRunStage`로 거절하고 아무것도 바꾸지 않는다. 피드 표에 이 실행의 소스가 없어도 거절한다(`CodeInvalidValue`) |
   | `IdentifyStep(lease)` | 자리가 가리키는 단위 하나를 한다. 단위의 쓰기와 자리의 기록이 한 트랜잭션이다 |
   | `IdentifyPlace(no)` | 시작했는지, 끝났는지, 어느 국면과 라운드와 회인지, Go를 기다리는지 |
   | `RebuildPage(lease)`, `PutRebuilt(lease, unit, rows)` | 키가 없는 레코드(미해결)를 Go에 주고, Go가 만든 키를 받는다 |
   | `AskPage(lease)`, `PutAnswers(lease, answers)` | 부모가 있는 새 객체를 Go에 주고, Go가 만든 id(또는 이름이 없다는 답)를 받는다 |
   | `IdentifyReport(no)` | 센 수(묶인 레코드, 새 객체, 새 키, 버린 키, 진 주장 등) |

   `RunIdentify`가 위를 이어 돌린다. Go가 하는 일은 `KeyMaker`(`PlanKeys`가 구현한다)다. 피드 표(`IdentifyStart.Feeds`)는 (소스 번호, 피드 번호)로 찾고, KG의 여러 소스의 계획을 함께 실을 수 있다(`ingest.IdentifyFeedsOf`). `KeyMaker`와 `PlanKeys`는 소스 번호로 계획을 고른다. 훅은 `Store`마다 하나(`identSeen`)라서 테스트가 서로 영향을 주지 않는다. 계산 중이 아닌 실행, 계산 테이블의 주인이 아닌 실행은 `CodeRunStage`로 거절한다.

2. **출발 레코드.** 2단계가 `calc_record`에 놓은 줄(새로 옴, 바뀜, 돌아옴, 풀림, 반복)과, `record_ref`가 미해결인 레코드(`why` = 미해결)다. 어떤 줄을 키가 있는 줄로 읽을지는 `why`로 판단하지 않는다. `why`는 우선순위로 하나만 남아 정보가 모자란다(ADR-0102). 받은 행에 키가 있으면(`stage_key`) 그것을 복사하고, 없으면(원본 레코드의 값을 그대로 쓰는 줄, `page`가 비어 있다) Go가 `raw_record`의 필드에서 키를 다시 만든다(P3: 정규 인코딩을 SQL로 다시 만들지 않는다). 받은 행에서 복사한 레코드는 Go에 주지 않는다. 키, 끝 키, 주장이 이미 복사되어 있어 다시 만들면 두 번 들어간다.

   **운명마다 식별에 참가하는지를 정한다.** 이 판단은 `why`를 읽는다(닫힘과 풀림은 레코드가 더는 객체를 받치지 않는다는 운명 자체다. "잡아 둠은 `calc_wait.expired`로" 규칙과 다르다).

   | 운명(`why`) | 식별 | 이유 |
   |---|---|---|
   | 닫힘(5), 풀림(6) | 객체를 만들지 않고 키를 묶지 않으며 Go에 주지도 않는다. 보고의 `Skipped`로 센다 | 사라졌거나 규칙이 거르는 레코드가 객체나 키를 만들면 안 된다. 객체의 지지가 빠지는 일은 4단계 이후의 몫이다 |
   | 반복(7) | 다른 레코드처럼 식별한다. 반복이라는 이유로 객체를 더 만들지 않는다 | 옛 `ingest.Runner`가 그렇게 했다. 자연 키마다 레코드 하나를 두고 나중 행이 이긴다(`run.go`의 `captured`). 앞의 행은 버려지고 `duplicate_key` 문제만 난다. 새 흐름의 비교(2단계)가 이미 나중 행을 레코드로 남긴다 |

   **출발 레코드는 계획이 주어진 소스의 것만 가져온다.** `record_ref`가 미해결인 다른 소스의 레코드는 그 소스의 계획이 없으면 키를 만들 수 없다. 건드리지 않고 `Counts.Left`로 센다(죽지도 않고 말없이 빠지지도 않는다).

3. **슬롯.** 3단계가 쓰는 작업 테이블은 모두 넣기만 한다(I3). 단위가 쓴 줄과 자리가 한 트랜잭션이라 지우는 문장이 없다.

   | 테이블 | 줄 | 쓰는 국면 | 읽는 쪽 |
   |---|---|---|---|
   | `calc_key`, `calc_end`, `calc_claim` | 받은 행의 키, 끝 키, 식별자 주장을 레코드 번호(`rid`)로 옮긴 것(`held` = 필터로 걸러진 행의 것) | 복사(`keys`), Go 재생성(`rebuild`) | 슬롯, 끝 풀기와 주장 |
   | `calc_slot` | 라운드에서 준비된 키(부모를 찾은 키): (`rid`, `src_no`, `feed_no`, `pid` = `rid`×1024+`feed_no`, `key_ord`, `type_no`, `ctx_no`, `sig_hash`, `round_no`, `found_no`). `src_no`는 레코드의 소스라서 피드 표를 (소스, 피드)로 찾는다 | `slot` | 묶음 찾기, 결정 |
   | `calc_label`, `calc_klabel` | 묶음 찾기의 회마다 쌍(`pid`)의 라벨과 키의 라벨(그 키를 가진 쌍의 가장 작은 라벨) | `klabels`, `labels` | 결정 |
   | `calc_ask` | 부모가 있는 새 객체: 부모의 id와 이 객체의 키, 그리고 Go의 답(`answer` = 참인 줄) | `decide`, `ask` | `PutAnswers` |
   | `calc_newobj` | 이 실행이 만든 객체: `obj_no`, `ref`(id), 타입, 만든 레코드 | `decide` | 적용(7단계), 다음 라운드의 부모 찾기 |
   | `calc_newkey` | 이 실행이 새로 묶은 키: (`type_no`, `ctx_no`, `sig_hash`) → `obj_no` | `decide` | 다음 라운드의 찾기, 적용 |
   | `calc_dropped` | 두 객체로 가는 새 키(버리는 키) | `decide` | 슬롯(그 키를 다시 쓰지 않는다) |
   | `calc_bind` | (`rid`, `feed_no`) → `obj_no`, `is_new` | `decide`, `lookup` | 4단계 |
   | `calc_ref` | 레코드가 낸 찾기 키의 결과(`role`, `def_no`, `target_no`, `state`) | `lookup`, `finish` | 정리(9단계)의 `record_ref` |
   | `calc_problem` | `identity_conflict`(두 객체를 가리키는 키), 이름 없음 | `decide`, `finish` | 정리 |

   `why`는 `CalcWhyUnresolved`(1)와 `CalcWhyExpanded`(2)를 3단계가 쓴다(확장은 4단계의 몫이라 이 변경은 쓰지 않는다). `calc_job` = 2(`CalcJobIdentify`)다.

4. **식별은 라운드로 한다(식별과 영향 집합 문서 2절).**
   - **찾기.** 라운드마다, 부모를 찾은 키만 슬롯이 된다. 키의 찾기는 `object_key`(기본키 `type_no`, `ctx_no`, `sig_hash`)와 이 실행이 새로 묶은 키(`calc_newkey`)에서 한다. 문맥이 없는 키는 라운드 1에서, 부모가 있는 키는 부모가 정해진 다음 라운드에서 준비된다(부모의 사슬은 네 단계까지 읽는다). 한 라운드에 슬롯이 하나도 없으면 가진 키만으로 한 번 더 해 보고(완화), 그래도 없으면 끝낸다. 라운드는 16번(`DefaultIdentifyRounds`)까지다.
   - **0, 1, 2개 이상을 찾은 경우.** 한 레코드의 키가 찾은 객체가 없으면 새 객체 후보이고, 하나면 그 객체이며, 둘 이상이면 앞의 키(피드가 키를 시도하는 순서)가 이기고 `identity_conflict` 문제를 하나 낸다. 새 키가 두 객체로 가면 버린다(`calc_dropped`).
   - **묶기.** 같은 키를 가진 쌍(`pid`)은 한 묶음이다. 연결 요소를 라벨 전파(가장 작은 라벨로 줄여 간다)로 찾는다. 한 회는 두 단계로 나눈다. 먼저 키마다 그 키를 가진 쌍의 라벨 가운데 가장 작은 것을 `calc_klabel`에 쓰고, 다음에 쌍마다 자기 키들의 라벨 가운데 가장 작은 것을 `calc_label`에 쓴다. 한 키를 많은 쌍이 가지는 경우(한 사이트에 레코드 수만 개)에도 키를 한 번 읽으므로 비용이 행 수에 비례한다(키를 가진 쌍끼리 조인하면 제곱이다. 이것을 먼저 해 보았다가 바꿨다). 라벨이 더 줄지 않으면 끝이다.
   - **지름길(pointer jumping).** 한 회는 쌍의 새 라벨(가장 작은 라벨)을 구한 뒤, 그 라벨을 쌍 번호로 보고 앞 회의 라벨을 세 번 따라간다(`labelJumps`). 라벨은 같은 묶음의 쌍 번호라서 따라간 자리도 같은 묶음이다. 그래서 키가 사슬로 이어진 묶음도 회 수가 사슬의 길이가 아니라 그 로그에 비례한다(별칭 사슬 27개 9회, 110개 11회, 440개 13회: `idn_chain_test.go`). 회는 40번(`DefaultIdentifyTurns`)까지다. 라벨은 회마다 모든 쌍을 쓴다(바뀐 것만 넣지 않는다. 바뀐 것만 넣으면 앞 회의 라벨을 "그 회 이하에서 가장 최근 줄"로 찾아야 해서 단위의 범위 읽기가 깨진다).
   - **한도까지 수렴하지 않은 라운드.** 마지막 회에 바뀐 라벨이 있으면(`Unsettled`) 온전함이 확인된 묶음만 묶는다. 온전함의 기준은 라벨이 같은 쌍의 묶음 가운데 그 묶음의 어떤 키도 마지막 회에 다른 라벨의 쌍이 가지고 있지 않은 것이다. 지시서의 "둘 이상 키를 가진 묶음을 묶지 않는다"보다 엄밀하게 해석했다. 키가 하나인 조각도 갈라진 사슬의 일부일 수 있어서 글자대로 하면 갈라선다. 나머지 묶음의 쌍은 미해결 참조(`RefUnresolved`)로 남기고 `Counts.Unsettled`와 `Counts.Unresolved`로 센다(한 줄이 쌍 하나다). 갈라서 따로 객체를 만드는 일은 없고 한 번 더 돌리면 다시 본다. 레코드마다 문제 한 줄을 내지 않는다(사슬 하나가 레코드 수만큼의 줄이 된다).
   - **실행에 한 줄의 문제(`identity_unsettled`)는 하지 않았다.** 문제 종류의 목록(`pgschema.ProblemKinds`)이 닫혀 있고, 잠긴 테스트(`pgingest/rules_test.go`)가 개수를 고정하며, 이 결정의 `amends_tests`에 없다. 종류를 더하려면 그 테스트의 개정, `calc_problem`과 `record_problem`의 CHECK를 넓히는 단계(생성기 7), 실행 단위의 문제 줄(`rid`가 없다)이 필요해 이 변경의 범위를 넘는다. 설계 책임자의 결정을 기다리며 그동안은 보고의 `Unsettled` 수가 그 일을 한다.
   - **대표 레코드.** 문맥 키만 있는 묶음의 객체는 그 묶음의 첫 키를 내는 레코드가 이름과 id를 정한다. 같은 키를 내는 레코드가 여럿이면 데이터셋 번호, 자연 키의 바이트 순으로 고른다(`rid`는 실행마다 달라서 쓰지 않는다. `idn_fates_test.go`가 행의 순서를 바꿔 확인한다).
   - **새 id.** 부모가 없는 객체의 id는 수신이 Go로 계산해 키에 실어 둔 후보(`cand_ref`)다. 부모가 있는 객체는 부모의 id가 정해진 뒤에야 만들 수 있어서 `calc_ask`로 Go에 묻는다. Go(`PlanKeys.Answer`)가 부모의 id를 문맥 자리에 넣어 `graph.NewObjectID`로 만든다. 이름의 규칙은 옛 흐름과 같다(`name` 속성이 있으면 그 값, 없으면 키의 첫 값). 이름이 없으면 `missing_name`이고 객체를 만들지 않는다. 개발 로더의 규칙과 다르면 개발 로더가 이긴다.
   - **식별자 주장.** 이미 그 식별자를 가진 객체가 지금도 주장하면 그 객체가 이기고, 아니면 가장 작은 `ref`가 이긴다. 진 쪽은 `RefLostClaim`이고 `identity_conflict` 문제를 낸다.
   - **걸러진 행과 만들지 않는 피드.** 규칙이 거른 행(`held`)과 객체를 만들지 않는 피드(`creates_objects` = 거짓)는 있는 객체만 찾는다. 새 키를 묶거나 별칭 키를 묶지 않는다. 옛 흐름은 이 경우에도 별칭 키를 묶었으므로 아래 표의 다른 점이다.

5. **스키마는 생성기 버전 7로 더한다(더하기만).** 6의 단계는 그대로 두고 7의 단계(`identifySteps`)만 `generations.go`에 더했다. 모두 `IF NOT EXISTS`라 두 번 돌려도 같다.
   - `calc_key.held`, `calc_end.held`, `calc_claim.held`(`boolean NOT NULL DEFAULT false`), `calc_bind.feed_no`(`integer NOT NULL DEFAULT 0`)
   - 새 테이블: `calc_slot`, `calc_label`, `calc_klabel`, `calc_ask`, `calc_newobj`. 모두 계산 테이블이라 `ComputeTables()`에 들어 있어서 다음 계산이 비운다.
   - 인덱스(문장별 인덱스는 결정 7). 받은 행의 파티션에는 BRIN, 계산 테이블에는 B-tree다.

6. **받은 행의 인덱스는 BRIN이다.** 복사는 (데이터셋, 페이지)의 범위로 받은 행(`stage_key`, `stage_end`, `stage_claim`)을 읽는다. 받은 행은 페이지 순서로 들어오므로 BRIN이 맞다. 수신의 쓰기 비용을 5백만 행(`BenchmarkIdnStageIndex`)에서 쟀다.

   | 인덱스 | 쓰는 시간 | 인덱스 크기 |
   |---|---|---|
   | 없음 | 5.8초 | 0 |
   | BRIN | 6.7초 (+16%) | 0.05 MiB |
   | B-tree | 8.8초 (+53%) | 31 MiB |

   B-tree는 수신의 쓰기를 절반 늘리고 인덱스도 크다. BRIN은 비용이 작고, 범위 읽기에 충분하다.

7. **문장마다 쓰는 인덱스, 그리고 단위의 읽기는 실행의 크기와 무관하다.** 모든 문장이 범위나 인덱스로 읽는다. 등록부 테이블뿐 아니라 작업 테이블(`calc_*`)도 그렇다. 단위의 문장은 자기 범위의 행을 읽고, 큰 테이블에서는 행마다 인덱스를 한 번 읽는다(`LATERAL`, `LIMIT 1` 또는 `OFFSET 0`: `absent`, `probe`, `scan`). 서버가 조인의 방향을 고르게 두면 해시나 병합, 안티 조인이 한쪽을 통째로 읽어 단위의 읽기가 실행의 크기에 비례했다(식별 검토의 측정: 10k에서 3만으로 가면 `calc_slot`, `calc_bind`, `calc_label`, `calc_klabel`을 단위마다 통째로 읽었다). 개수도 문장이 쓴 줄의 수를 더해 가고(`unitStep.to`), 끝에 테이블을 세지 않는다. `idn_plan_test.go`가 단위의 문장마다 `EXPLAIN (ANALYZE)`로 읽은 행을 재고, 모든 테이블에서 읽는 행이 단위의 크기에 비례하고 실행이 3배가 되어도 늘지 않는지를 확인한다.

   | 문장 | 인덱스 |
   |---|---|
   | 미해결 레코드 찾기 | `record_ref_open`(상태가 해결이 아닌 줄의 부분 인덱스), `raw_record`와 `source_dataset`의 기본키 |
   | 키, 끝 키, 주장 복사 | `stage_*_page`(BRIN), `calc_record_row` |
   | 슬롯 만들기 | `calc_key_rid`, `calc_record`의 기본키, `object_key`의 기본키, `calc_newkey_key`, `calc_bind_pair`, `calc_ref_pair`, `calc_dropped`의 기본키 |
   | 키 라벨, 쌍 라벨 | `calc_slot_key`, `calc_slot_pid`, `calc_label_pid`, `calc_klabel_key` |
   | 결정(새 객체, 묶기, 새 키) | `calc_label_label`, `calc_slot_pid`, `calc_slot_key`, `calc_key_rid`, `calc_bind_pair`, `calc_newobj_ref`, `calc_newkey_key`, `calc_ref_pair`, 질문은 `calc_ask`의 기본키 |
   | 찾기만 하는 레코드, 주장, 끝내기 | `calc_key_rid`, `calc_record`의 기본키, `calc_bind_pair`, `calc_claim_value`, `object_identifier`, `calc_ref_pair`, `calc_newobj`의 기본키 |

   작업 테이블은 넣기만 해서 서버의 통계가 따라오지 않으므로, 국면이 바뀔 때(복사 뒤, 슬롯 뒤, 라벨 뒤) `ANALYZE`를 한다(표본만 읽어 비용이 작다). 이것이 없으면 계획이 한쪽을 통째로 읽는 조인을 골라 제곱의 비용이 되었다(측정했다). 단위가 쓰는 트랜잭션에서 중첩 루프를 끄는 방법은 쓰지 않는다(ADR-0102의 방법은 단위가 한 범위만 읽을 때 맞았고, 여기는 문장마다 조인의 모양이 다르다).

8. **이어 가기는 단위 단위다.** 단위가 쓴 줄과 자리가 한 트랜잭션이라 끊긴 단위는 아무것도 남기지 않고, 이어받은 서버가 같은 자리에서 같은 단위를 한다. 문서의 "그 라운드의 처음부터 (결과 테이블을 그 라운드만 비우고)"보다 잘게 나눈 것이고, 지우는 문장이 없다. 시도 번호가 다른 쓰기는 아무것도 바꾸지 않는다. 이 변경에서 라운드 단위로 지우는 문장이 필요한 국면은 없었다. 계산 테이블의 주인(`calc_owner`)이 이 실행이 아니면 이어 가지 않는다.

9. **선언 리비전(F5).** 선언은 리비전마다 저장하지 않는다(`source_dataset`에는 현재의 `decl_rev`만 있다). 그래서 원본 레코드의 `decl_rev`가 현재와 다른 레코드는 식별에서 빼고 새 참조를 쓰지 않는다. 이미 해결된 묶임은 건드리지 않는다.

10. **값 관문.** 식별이 만드는 값은 해시나 고정 폭의 숫자와 id다(`sig_hash` 16바이트, `obj_no`, `ref`). 이름은 3단계가 저장하지 않는다(`calc_newobj`에 이름 열이 없다). 이름의 바이트 상한(`graph.MaxNameBytes`)은 키를 만들 때 이미 지키고, 4단계가 이름을 저장할 때 다시 본다.

11. **옛 흐름과 견준다(`idn_parity_test.go`, `idn_parity_more_test.go`).** 옛 `ingest.Runner`를 `ManualClock`으로 돌려 같은 입력으로 견준다. 6단계의 사례(새 객체, 별칭 키, 바뀐 별칭, 이미 있는 키가 찾는 경우, 키끼리 이어지는 경우, 두 객체를 가리키는 키)는 사례마다 살아 있는 객체의 수가 같고, 묶임이 한 키에서 나오는 사례는 id도 같다. 넓힌 견주기는 데이터셋 둘, 키 셋(제조사와 일련번호, 시스템 uuid, 식별자 `bmc_mac`), 반복되는 키, 13시간 간격의 일곱 번의 실행에서 한 레코드가 사라졌다가 닫히고 다시 오는 경우를 한 흐름으로 돌려, 실행마다 지금까지 만든 객체의 id가 같은지(닫힌 객체까지) 본다. 풀림과 다른 소스의 미해결 레코드는 옛 흐름이 다루지 않아서(소스마다 따로 돌고 규칙으로 거르는 일이 없다) 견줄 수 없고 `idn_fates_test.go`와 `idn_sources_test.go`가 이 결정의 규칙으로 확인한다.

    | 다른 점 | 옛 | 새 | 이유 |
    |---|---|---|---|
    | 순서 | 먼저 온 행이 객체의 이름과 id를 정한다(순서에 따라 결과가 다르다) | 결과는 순서와 상관없다(I1). 묶음의 객체는 모델의 첫 키(같으면 서명이 작은 키)가 정한다 | 옛 흐름이 순서에 기대는 것은 규모에서 쓸 수 없다 |
    | 두 객체로 가는 새 키 | 앞의 객체에 묶는다 | 어느 쪽에도 묶지 않고 버린다 | 순서와 상관없게 하는 규칙이다 |
    | 걸러진 행과 만들지 않는 피드의 별칭 키 | 묶는다 | 묶지 않는다(찾기만 한다) | 규칙이 거른 행이 레지스트리의 키를 늘리면 안 된다 |
    | 마지막 실행까지 살아 있던 객체 | 그 객체만 후보 | 식별 색인의 모든 객체 | 색인이 이력이다(식별과 영향 집합 2.1) |
    | 닫힌 레코드, 풀린 레코드 | 레코드가 없어져 객체를 만들지 않는다 | 식별에서 뺀다 | 지지가 빠지는 일은 4단계 이후다(결정 2) |
    | 반복되는 키 | 나중 행이 레코드이고 문제 `duplicate_key`를 낸다 | 나중 행이 레코드이고 객체는 하나다(문제 줄은 2단계가 낸다) | 같은 결과다 |

## 측정(보고만, 한 번)

레코드 300,000개, 바뀐 것 10%(별칭 키를 바꿈)와 새 것 1%, 다섯 단계의 문맥 키(사이트, 건물, 층, 방, 랙)를 가진 소스다. 첫 실행은 전부 새 것이고, 둘째 실행의 식별을 쟀다. 데이터는 Go가 생성한 CSV를 수신으로 넣었다(`generate_series`로 만든 계산 상태를 직접 쓰지 않는다. 문맥 키의 id와 키의 서명을 SQL로 흉내 내면 P3를 어기기 때문이다). 2코어 컨테이너의 PostgreSQL 18이고 다른 부하가 있는 때의 값이라 어림으로만 읽는다. 100만 개로 재려 했으나 디스크 여유(8 GB)가 모자라 30만 개에서 멈췄다(100만 개의 첫 시도에서 아래 두 문제를 찾아 고쳤다).

| 레코드 | 바뀌거나 새 것 | 둘째 실행: 수신과 비교 | 둘째 실행: 식별 | 단위(10만 쌍)의 가장 긴 문장 |
|---|---|---|---|---|
| 20,000 | 2,200 | 1.1초 | 약 1초 | 1초 안 |
| 30만 | 33,000 | 14.8초 | 약 27초 | 슬롯 만들기 약 4초 |

단위의 가장 긴 문장이 한도(임대 60초의 3분의 1)의 5분의 1 안이다. 100만 개의 첫 시도에서 단위 하나가 6분 걸린 문장이 둘 있었고(답을 쓰는 단위의 "부모가 있는 객체 만들기"와 "새 키 묶기"), 원인은 청크마다 10만 개의 라벨이 든 배열로 `= ANY(배열)`을 하던 것(청크 크기의 제곱)이다. 라벨을 행으로 풀어 조인으로 바꿔 고쳤다. 비용이 행 수에 비례하는지는 위 두 줄과 단위 문장이 일정한 것으로 보았고, 100만 개의 값은 재지 못했다.

### 단위의 읽기가 실행의 크기와 무관한지(식별 검토 4)

위 측정 뒤에 작업 테이블(`calc_*`)을 읽는 문장이 실행의 크기에 비례해 읽는 것이 검토에서 드러났다(10k에서 3만으로 가면 단위마다 `calc_slot`, `calc_bind`, `calc_label`, `calc_klabel`을 통째로 읽었다). 모든 문장을 행마다 인덱스 한 번 읽는 `LATERAL`로 바꾸고 `EXPLAIN (ANALYZE)` 테스트를 모든 테이블로 넓혔다. 단위가 100개 레코드일 때 한 단위가 읽은 행(가장 많이 읽은 단위)이다. 같은 소스를 10,000개와 30,000개로 둘째 실행(10%가 바뀌고 1%가 새 것)을 했다.

| 문장 | 읽은 테이블 | 10,000개 | 30,000개 |
|---|---|---|---|
| 슬롯 만들기 | `calc_key`, `calc_record`, `calc_bind` | 600, 600, 600 | 600, 600, 600 |
| 쌍 라벨 | `calc_label`, `calc_klabel` | 317, 102 | 325, 102 |
| 키 라벨 | `calc_slot`, `calc_label` | 229, 229 | 251, 251 |
| 새 키 묶기 | `calc_slot`, `calc_label`, `calc_newkey` | 357, 310, 200 | 283, 283, 200 |
| 끝내기 | `calc_bind`, `calc_key`, `calc_record` | 600, 600, 600 | 600, 600, 600 |

기본 단위(10만 쌍)로 잰 둘째 실행의 식별 시간이다(2코어, 다른 부하가 있는 때의 어림): 10,000개(1,100개가 바뀌거나 새 것) 1.4초, 30,000개(3,300개) 3.7초, 100,000개(11,000개) 13.7초. 단위 하나의 가장 긴 문장은 슬롯 만들기로 10만 개에서 4~6초이고, 한도(임대 60초의 3분의 1) 안이다. 처음에 `LATERAL`로만 바꾸자 슬롯 만들기에서 서버가 "준비됨"을 가리는 조인을 단위의 제곱짜리 중첩 루프로 골라 3만 개가 10분을 넘겼다. 창 함수(`bool_and(...) OVER (PARTITION BY rid, feed_no)`)로 바꿔 고쳤다. 검토의 조건과 달리 테스트 데이터는 한 키의 쌍 수가 실행의 크기에 따라 늘지 않게 고쳤다(사이트와 건물을 레코드 2천, 1천 개마다 하나로 하고, 새 레코드를 레코드 백 개에 하나씩 퍼뜨렸다). 한 키의 쌍 수가 곧 읽는 양이 되는 것은 키 라벨의 본성이다(알려진 한계).

## 알려진 한계

- 한 묶음(한 키를 가진 쌍)이 한 단위보다 크면 단위가 그 묶음만큼 커진다(키를 쪼개면 묶음이 갈라진다). 한 사이트에 레코드 수만 개가 있는 경우에도 읽는 행은 묶음의 크기에 비례할 뿐이다.
- 키 하나를 가진 쌍이 많으면 키 라벨의 단위는 그 키의 쌍을 모두 읽는다(키를 쪼갤 수 없다). 읽는 양은 그 키의 쌍의 수에 비례할 뿐 실행의 크기와는 무관하다. 그래서 `EXPLAIN` 테스트의 데이터는 한 키의 쌍 수가 실행의 크기에 따라 늘지 않게 만든다(사이트 2천, 건물 1천 레코드마다).
- 가장 작은 라벨의 전파는 라운드마다 모든 쌍을 회마다 다시 쓴다. 쓰는 양이 (쌍의 수)×(회 수)이고 회 수는 지름길로 로그다. 바뀐 라벨만 쓰는 방식(문서 2.2)은 앞 회의 라벨을 "그 회 이하에서 가장 최근 줄"로 찾아야 해서 단위의 범위 읽기를 깨뜨려 쓰지 않았다.
- 선언의 리비전이 달라진 레코드는 식별하지 않는다(결정 9). 선언을 리비전마다 저장하게 되면 다시 본다.

## 결과

- 4단계는 `calc_bind`, `calc_newobj`, `calc_newkey`, `calc_ref`, `calc_problem`을 읽는다. 등록부와 `record_ref`는 정리(9단계) 전까지 바뀌지 않는다.
- 상태 기계(I-7)는 `RunIdentify`를 `RunRawDiff` 다음에 부르면 된다.
