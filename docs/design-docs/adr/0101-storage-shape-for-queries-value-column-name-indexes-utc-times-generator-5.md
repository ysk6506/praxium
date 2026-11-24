---
title: ADR-0101 질의 스키마의 설계에 저장의 모양을 맞춘다 — 변동 값은 값 열 하나, 이름 인덱스, 시각 값의 형식, 생성기 버전 5
kind: decision
status: active
enforced_by:
  - TLOCK001
  - backend/internal/pgstore/kgvalue_test.go
  - backend/internal/pgstore/typeindex_test.go
  - backend/internal/pgstore/schema_values_test.go
  - backend/internal/pgschema/valuemove_test.go
  - backend/internal/graph/validate_utc_test.go
  - backend/internal/graph/apply_number_test.go
  - backend/internal/graph/value_gate_test.go
  - backend/internal/ingest/valuegate_test.go
  - backend/internal/pgstore/valuegate_test.go
  - backend/internal/ingest/utctime_test.go
  - backend/internal/pgstore/numberform_test.go
touches_protected:
  - docs/design-docs/adr/0085-new-kg-storage-implementation-decisions-and-the-schema-plan-unit.md
  - docs/design-docs/adr/0089-new-kg-storage-the-kg-stands-on-its-own-schema.md
  - docs/design-docs/adr/0095-ingestion-tables-in-the-schema-plan.md
  - docs/design-docs/adr/0099-staged-applier-and-the-volatile-step.md
  - docs/product-specs/requirements-model-storage.md
  - harness/config/test-lock.json
updates_docs:
  - docs/design-docs/storage-shape.md
  - docs/product-specs/requirements-model-storage.md
amends:
  - adr: "0085"
    what: 결정 3의 변동 값의 등급과 근거와 출처를 `prov`에 두는 것에서 값 자체를 값 열에 두는 것으로
  - adr: "0089"
    what: 구현 규칙의 변동 값의 정확한 값이 `prov`에 있고 타입 있는 컬럼은 복사본이라는 것
  - adr: "0095"
    what: 결정 3의 `stage_volatile`의 `entry`가 `object_state`의 `prov`에 들어갈 항목이라는 것(값은 열로 가고 시각 키는 `t`다)
  - adr: "0099"
    what: 결정 7의 변동 값 쓰기를 `prov`에 합쳐 쓰는 것에서 값 열과 `prov`에 쓰는 것으로, 같음의 견줌을 열의 형으로
amends_tests:
  - go:backend/internal/pgschema::TestControlReservesThePartitionPrefix
  - go:backend/internal/pgschema::TestDiffFollowsAdditions
  - go:backend/internal/pgschema::TestIngestHeldDiffFromGeneratorThreeAddsTheColumnHeldToTheThreeTablesOfTheRowsReceivedAndNothingElse
  - go:backend/internal/pgstore::TestRsmALeaseIsGivenAfterTheAbandonThatWasCutIsEnded
  - go:backend/internal/pgstore::TestRsmALeaseIsGivenAfterTheRestoreThatWasCutIsEndedAndTheKGIsOpenAgain
  - go:backend/internal/pgstore::TestRsmALeaseOfAKGWithNoWorkHalfDoneIsGivenAtOnce
  - go:backend/internal/pgstore::TestRsmTheServerEndsTheWorkOfEveryKGAndOneThatCannotBeEndedDoesNotStopTheOthers
  - go:backend/internal/pgstore::TestV2cGuardChecksInOrderAndTheHeadRoundTrips
  - go:backend/internal/pgstore::TestV2cLeaseIsTakenRenewedAndReleasedByTheDatabaseClock
  - go:backend/internal/pgstore::TestV2cLeaseRunsOutByTheClockOfTheCallerWhenTheStoreIsAskedTo
  - go:backend/internal/pgstore::TestV2cResolveDecidesAnUnknownCommitByTheHead
  - go:backend/internal/pgstore::TestV2dEveryOperationFailsOnAClosedPool
  - go:backend/internal/pgstore::TestV2dReadsFailOnRowsThatHoldWhatTheStoreDidNotWrite
  - go:backend/internal/pgstore::TestV2dReadsFailWhenATableTheyNeedIsMissing
  - go:backend/internal/pgstore::TestV2eALeaseAndAHeadThatAreDamagedFail
  - go:backend/internal/pgstore::TestV2eAServerThatHoldsAnotherModelRefusesTheKG
  - go:backend/internal/pgstore::TestV2eReadPointsAndVersions
  - go:backend/internal/pgstore::TestV2eTheCatalogRefusesDamagedRowsAndAnUnknownModel
  - go:backend/internal/pgstore::TestV2mAChunkWithAValueThatHasNoPlaceIsRefused
  - go:backend/internal/pgstore::TestV2mASealThatIsNotReadyOrDoesNotFollowIsRefused
  - go:backend/internal/pgstore::TestV2mAnUnknownOutcomeOfTheStartOfAWorkIsDecidedByTheHead
  - go:backend/internal/pgstore::TestV2mWritesAndFeedsFailWhenATableIsGone
  - go:backend/internal/pgstore::TestV2rCatalogListsGetsAndDeletesAKG
  - go:backend/internal/pgstore::TestV2rReadsLinksIdentifiersAndVolatileValues
  - go:backend/internal/pgstore::TestV2rReadsTheNewestAndThePastStates
  - go:backend/internal/pgstore::TestV2rRefusesReadsThatCannotBeAnswered
  - go:backend/internal/pgstore::TestV2wAChangeOfValuesAloneDoesNotTouchTheRowsOfTheObjectAndTheRowsAreWrittenInOrder
  - go:backend/internal/pgstore::TestV2wARefThatCameBackWithAnotherTypeIsRefused
  - go:backend/internal/pgstore::TestV2wARetiredObjectComesBackWithTheSameNumberAndTheKeyStaysBound
  - go:backend/internal/pgstore::TestV2wAWriteIsRefusedWhenTheSchemaWasMadeFromAnotherModel
  - go:backend/internal/pgstore::TestV2wAbandonTakesTheChunksOutOneNumberAtATimeAndLeavesNoTraceAndAnInterruptedOneIsEnded
  - go:backend/internal/pgstore::TestV2wBulkReadsGiveWhatReadsOneAtATimeGive
  - go:backend/internal/pgstore::TestV2wRestoreClosesTheKGUntilItEndsAndAnInterruptedOneIsResumed
  - go:backend/internal/pgstore::TestV2wTheSettingsOfTheTablesAreInForce
test_change_reason: 변동 값의 진실을 prov에서 값 열로 옮기고(REQ-760) 이름 인덱스와 시각 형식을 더하면서(REQ-761, REQ-762) 생성기 버전이 5가 되어 버전과 단계 목록의 단언 값이 바뀐다. 값이 바뀌는 것만 바꿨고 옛 단계를 확인하던 단언은 남겼다. 값을 직접 심던 테스트용 시드 한 곳이 값 열에 값을 넣게 바뀌어 그것을 쓰는 테스트의 지문이 바뀌었고 단언은 그대로다.
decision_hash: b04c2a796e2bd13d
---
# ADR-0101 질의 스키마의 설계에 저장의 모양을 맞춘다 — 변동 값은 값 열 하나, 이름 인덱스, 시각 값의 형식, 생성기 버전 5

## 맥락

질의 스키마([질의 스키마](../query-schema.md))의 뷰는 변동 값을 상태 테이블의 값 열에서, 시각 값을 불변 함수 `praxium_ts`로, 이름과 참조로 찾는 질의를 타입 테이블의 인덱스로 읽는다고 가정한다. 지금 저장은 그 가정과 네 곳이 어긋난다. (가) 값 열 `a<번호>`와 `a<번호>_at`은 만들어지지만 쓰는 코드가 없고 값은 `prov` JSON에만 있다(ADR-0089 구현 규칙). 그래서 `oper_state`의 인덱스(`object_state_a<번호>`)는 아무것도 가리키지 않는다. (나) `prov` 항목의 시각 키가 `s`이고 문서(`doc`)의 항목은 `t`다. (다) `t_<type_no>`에는 기본키뿐이라 이름으로 찾기가 테이블 훑기다. (라) 시각 값이 UTC에 끝이 `Z`인지 확인하는 곳이 없다. 적용기의 검사(`graph.validTime`)는 RFC 3339면 받았고, 오프셋이 있는 시각도 저장에 갔다. 이 변경은 질의 스키마를 만들기 전에 저장의 모양을 그 설계에 맞춘다. 질의 스키마, 실행기, 내장 읽기는 하지 않는다.

ADR-0089는 변동 값의 정확한 값을 `prov`에 두고 타입 있는 컬럼을 복사본으로 정했다. 이유는 두 가지였다: 수량의 맵 형태와 시각의 오프셋이 타입 있는 컬럼에 손실 없이 들어가지 않는다는 것이다. 이 결정은 값의 진실을 열로 옮기면서 두 이유를 값 검사로 없앤다(결정 5).

## 결정

1. **변동 값의 진실은 값 열 하나다**(REQ-760). 변동 속성의 값은 상태 테이블(`object_state`, `s_<type_no>`)의 값 열 `a<번호>`에 쓰고, 관측 시각은 열 `a<번호>_at`에도 같은 입력에서 함께 쓴다(질의에서 시각으로 거르고 정렬하는 쪽). `prov`에는 값이 없다. 항목은 `{"<속성 번호>": {"g": 등급, "b": 근거 종류, "t": 관측 시각, "r": 출처}}`이고 `t`와 `r`은 비어 있으면 빠진다. 값이 없는 항목(알 수 없음)은 값 열이 NULL이고 `prov`의 항목이 있다. 읽을 때 `prov`에 그 속성의 항목이 없으면 값이 없는 것이고 열은 보지 않는다. 쓰는 문장은 열마다 하나다: 속성 하나의 값들을 `unnest`한 배열로 받아 `INSERT ... ON CONFLICT (obj_no) DO UPDATE`로 값 열, 시각 열, `prov`(`prov || EXCLUDED.prov`)를 함께 쓴다. 값은 글자로 보내 문장 안에서 열의 형으로 바꾼다(`ColumnType`: 시각은 `praxium_ts`, 나머지는 `::형`). 행 단위 길(`kgwrite_rows.go`)과 준비 테이블 길(`kgstaged_volatile.go`)이 같은 쓰기 함수(`kgvalue.go`)를 쓴다.
2. **같은 값을 쓰지 않는 규칙(REQ-566, ADR-0099 결정 7)은 열의 값과 `prov`를 함께 견줘 지킨다.** 준비 테이블 길은 구간의 값을 속성마다 `unnest`해서 저장된 행과 조인해, 값 열이 `IS DISTINCT FROM`이거나 `prov`의 항목이 없거나 `g`나 `b`가 다른 줄만 쓴다. 견줌은 열의 형으로 하므로 같은 수가 다른 글자(`5`와 `"5.0"`)로 와도 같은 값이다. 시각 `t`와 출처 `r`만 다르면 쓰지 않는다. 행 단위 길의 같음 판단은 그래프 계층(`graph.chunkBuilder`)이 읽은 값의 표지로 하고, 읽은 값은 아래 왕복의 표대로 돌아온다.
3. **`prov`와 `doc`의 시각 키는 `t`로 맞췄다.** 출처는 `r`로 이미 같았다. `prov`의 옛 키 `s`를 `t`로 바꿨고(`provEntry`, 코드의 `Seen`), 뷰가 바꾸는 처리 경로은 쓰지 않는다. `stage_volatile.entry`는 준비 값이 `v`(값)를 갖고 `g`, `b`, `t`, `r`을 갖는다. 이 테이블에 쓰는 쪽(2단계 이후의 수집 흐름)은 `t`를 써야 한다. 옛 키 `s`는 읽지 않는다.
4. **`t_<type_no>`에 이름 인덱스 둘을 둔다**(REQ-761). `t_<type_no>_name`은 `(name)`이고 정확한 일치와 이름순을, `t_<type_no>_name_ci`는 `(lower(name) text_pattern_ops)`이고 대소문자를 무시한 접두어 일치(`lower(name) LIKE 'ab%'`)와 같음(`lower(name) = 'ab'`)을 인덱스에 태운다. 정렬 규칙이 C가 아니면 일반 연산자 클래스로는 `LIKE` 접두어가 인덱스를 못 타서 `text_pattern_ops`를 쓴다. `ref` 인덱스는 두지 않는다: `ref`는 등록부 `object`의 인덱스가 맡고 타입을 알려 주며, 타입 테이블은 번호로 읽는다. 새 타입의 테이블은 만들 때 인덱스도 만들고(같은 트랜잭션, 빈 테이블), 있는 KG의 테이블에는 온라인(`CREATE INDEX CONCURRENTLY IF NOT EXISTS`)으로 만든다.
5. **시각 값은 UTC에 끝이 `Z`이고 초의 소수점 아래가 6자리까지에 끝이 0이 아닌 한 가지 형식만 받는다**(REQ-762). 확인하는 곳은 적용기의 값 검사(`graph/value_gate.go`의 `TimeFits`)이고, 이전에는 `time.RFC3339`로 읽히면 모두 받았다. 지금은 `time.RFC3339Nano`가 쓰는 글과 같아야 한다(오프셋, `+00:00`, 시간대 없는 글, 7자리 이상, 끝이 0인 소수는 거절). 데이터베이스가 마이크로초까지만 가지므로 이 형식이어야 열에서 읽은 글이 쓴 글과 같다. 잘못된 값은 그 레코드의 문제이고(`CodeBatchInvalid`) 저장에 가지 않는다. 변동 값의 관측 시각(`SeenAt`)도 같은 검사를 받고 빈 글은 받는다. 같은 이유로 **변동 속성의 수량은 수이고 맵이 아니다**: 값 열이 `numeric` 하나라서 단위가 든 맵은 받지 않는다(단위는 모델에 있다). 변동이 아닌 속성의 수량 맵은 지금처럼 문서에 둔다.
6. **생성기 버전을 4에서 5로 올리고 `Diff`의 더하기 단계로 옛 KG를 맞춘다**(REQ-763, ADR-0095 결정 10의 길). 단계 목록은 `generatorAdditions[5]`다(`generations.go`): 상태 테이블마다 값을 옮기는 `UPDATE` 하나(트랜잭션 안, 종류 `data`), 이어 옛 타입마다 이름 인덱스 둘(`Concurrent`). 더하기 단계가 매니페스트에서 타입과 상태 속성을 읽어야 해서 단계 함수가 옛 매니페스트를 받게 했고, `Diff`는 단계를 트랜잭션 안과 밖으로 갈라 놓는다. 스키마 실행기는 `Step`의 SQL을 그대로 돌리므로 데이터 단계를 그대로 받는다(새 길이 필요 없다).
7. **옛 모양의 데이터(`prov`에 값이 든 행)는 맞추는 단계가 옮긴다.** 문장 하나가 한 행을 읽은 그대로 열(`a<번호>`, `a<번호>_at`)에 값과 시각을 쓰고(`Expr`와 같은 식으로 JSON의 `v`를 열의 형으로 바꾼다), `prov`의 항목에서 `v`를 빼고 `s`를 `t`로 바꾼다. `v`나 `s`가 든 항목이 하나라도 있는 행만 고치고(이미 옮긴 행은 건드리지 않아 다시 돌려도 같다), 항목이 없는 행과 새 모양의 행은 그대로다. 오프셋이 있는 옛 시각은 같은 순간의 UTC로 옮긴다(버리는 것이 없다). 열의 형으로 바꿀 수 없는 옛 값(수량 맵)과 열이 없는 속성 번호의 값이 있으면 문장이 실패하고(오류에 속성 번호가 있다) 그 KG는 맞춰지지 않은 채 D19로 거절된 상태로 남는다(`AdjustOldSchemas`가 KG마다 이유를 알린다). 값을 버리고 계속하지 않는다. 시각이 아닌 옛 관측 시각 글은 `prov`의 `t`에 그대로 두고 `_at` 열은 비운다(값이 아니라 관측 기록이다). 값이 빈 `t`와 `r`은 옮길 때 뺀다(새 쓰기와 같은 모양).
8. **인덱스가 반쯤 만들어진 채 멈춘 KG의 끝내기.** 옛 KG가 맞춰지는 도중에 멈추면 기록은 `index_pending`이고 매니페스트는 이미 5다. 서버 시작의 맞추기(`AdjustOldSchemas`)는 현재 생성기의 KG라도 상태가 `ok`가 아니면 정합을 끝낸다(`building`이 한창이면 다른 실행이 잡은 것이라 건너뛴다). 온라인 단계의 정합(`reconcileSteps`)은 매니페스트의 모든 타입의 이름 인덱스 둘이 있고 유효한지 보고 모자란 것을 만든다. 서버 시작 경로로 테스트한다.
9. **부모 타입의 테이블**(REQ-764). 객체는 자기 타입의 테이블에만 들고 부모 타입의 테이블은 만들어지지만 비어 있다. 테스트가 표준 모델의 `PowerRedundancyGroup`(부모 `RedundancyGroup`)의 객체를 적용해 확인하고, 모든 타입에서 테이블의 행 수가 등록부의 그 타입 객체 수와 같음을 본다. 표를 줄이는 변경은 하지 않았다. 부모 타입의 뷰가 자식 뷰의 `UNION ALL`이어야 중복 없이 모든 객체를 낸다는 사실은 질의 스키마의 구현(B3-1)이 쓴다.
10. **시각은 수집의 가장자리에서 UTC로 맞춘다**(REQ-762). 저장 쪽 검사는 그대로 두고, 오프셋이 있는 시각(`+09:00`)을 보내는 소스가 레코드의 문제로 빠지지 않게 `ingest`의 셀 변환(`cells.go`의 `utcTime`)이 RFC 3339로 읽힌 시각을 UTC의 정규 글(`time.RFC3339Nano`, 마이크로초까지)로 바꿔 넘긴다. 같은 순간이 다른 글로 와도 같은 값이어서 해시와 같은 값 판단이 흔들리지 않는다. 날짜만 있는 칸은 지금처럼 자정 UTC다. 시각을 만드는 다른 곳(개발 적재, 세계 생성, `pgsource`, 수집의 관측 시각)은 이미 UTC의 정규 글이라 바꾸지 않았다.
11. **수의 같은 값 판단은 값으로 한다**(REQ-566). 행 단위 경로도 수(`number`)와 수량(`quantity`)을 값으로 견준다(`graph/apply_number.go`: 수량은 정확한 분수, 수는 가장 가까운 float의 가장 짧은 글). 같은 수가 `"1.10"`, `"1.1"`, `5`, `"5.0"`처럼 다른 글로 거듭 와도 두 번째부터는 쓰지 않고 변경 번호도 쓰지 않는다. 준비 테이블 경로가 SQL로 하던 것과 같은 결과다.
12. **도메인 검사(graph의 값 검사)가 저장이 받을 수 없는 값을 모두 거절하는 하나의 관문이다**(REQ-760, REQ-762). 관문을 지난 값은 열을 왕복해 같은 값으로 돌아온다. `graph.NumberFits`, `graph.TimeFits`, `graph.MaxNameBytes`가 그 규칙이고 수집의 셀 변환도 같은 함수를 쓴다. 수(`number`, double precision)는 십진 글(끝에 네 자리까지의 지수 허용)이고 유한한 float64 범위여야 하며, 입력의 십진 값과 float의 가장 짧은 십진 글이 수로 같지 않으면 거절한다(9007199254740993, 0.30000000000000001, 1e-400, 1e400, 16진 글. 메시지가 정수나 수량 타입을 권한다). `"1.10"`이 1.1이 되는 것처럼 같은 수는 받는다. 정수 값은 절댓값이 2^53 이하여야 한다. 수량은 지수 없는 십진 글(numeric)이나 정수다(16진, 지수, NaN은 거절). 글의 길이는 400자 이하다. 시각은 연도 1부터 9999까지다(`praxium_ts`가 읽는 범위). 이름은 1,024바이트 이하다(이름 인덱스의 행이 한 페이지에 들어가야 한다. 4,000자의 이름이 `index row size exceeds` 오류를 낸 것을 재현했다). 틀린 값은 그 레코드의 문제(`CodeBatchInvalid`)이고 내부 오류가 아니다. 관문을 지나고도 열이 받지 못하는 값(SQLSTATE 22 계열)은 쓰기가 `CodeBatchInvalid`로 돌려 준다. 읽을 때 수는 정수로 읽히면(2^53 이하) 정수, 아니면 지수 없는 가장 짧은 십진 글(절댓값 1e21 미만)로 돌아온다. 기본 종류마다 "관문을 지난 값은 DB를 왕복해 같은 값"임을 고정 시드의 무작위 입력과 경계 사례로 테스트가 확인한다.
13. **준비 테이블 길(`stage_volatile`)을 채우는 코드(수집 쪽, I-5)는 같은 관문을 지나야 한다**(REQ-760). 준비 테이블 길의 적용(`ApplyStagedVolatile`)에는 값 검사가 없고(`praxium_ts`는 `'now'`, `'yesterday'`, 시간대 없는 글도 받고 그 글은 세션 시간대에 따라 다른 순간이 된다), 채우는 코드가 아직 없어 지금 해당하는 경로은 없다. 채우는 쪽이 생기면 값(`v`)과 관측 시각(`t`)을 `graph.NumberFits`와 `graph.TimeFits`로 걸러야 한다.
14. **맞춘 스키마를 옛 빌드의 쓰기에서 지킨다**(REQ-763). 모든 쓰기의 첫 가드(`kgGuard`)가 기록의 생성기 버전을 다시 읽어 이 빌드의 것과 다르면 `CodeSchemaMismatch`로 거절하고 캐시를 버린다. 이 빌드 이후의 빌드에서는 옛 모양 쓰기가 막힌다. 이 변경 이전의 빌드는 이 가드가 없으므로, 4에서 5로 올리는 이 한 번은 옛 빌드를 모두 내린 뒤 새 빌드를 올려야 한다(옛 빌드가 맞춘 KG에 `prov`의 `v`로 쓰면 그 값은 "알 수 없음"으로 읽히고 다시 옮겨지지 않는다). 세대를 올리는 길은 복원의 사건 기록과 섞여서 택하지 않았다.

## 확인한 사실

**값의 왕복**(`TestVcolEveryKindOfValueGoesThroughItsColumnAndComesBack`): 값을 글자로 보내 열의 형으로 바꾸고 다시 글자로 읽어 포트의 값으로 돌린 결과다.

| 값의 형 | 열의 형 | 보낸 값 | 돌아온 값 |
|---|---|---|---|
| 문자열, 열거, 참조 | `text` | 같음 | 같음 |
| 정수 | `bigint` | `-9007199254740993`(int64) | 같음 |
| 불리언 | `boolean` | `true`, `false` | 같음 |
| 수(`number`) | `double precision` | `"12.5"`, `5`(int64), `"-0.25"` | `"12.5"`, `5`(int64), `"-0.25"` |
| 수량 | `numeric` | `"64.5"`, `"1.10"`, `3`(int64) | `"64.5"`, `"1.10"`, `3`(int64) |
| 시각 | `timestamptz` | `2030-01-01T00:00:00Z`, `...00.5Z`, `...00.123456Z` | 같음 |
| 목록, 맵 | `jsonb` | 같음 | 같음 |

열이 가진 것은 값이지 쓴 글자가 아니라서 형태가 하나로 모이는 곳이 있다: 정수로 읽히는 수는 정수로 돌아온다(`"5"`로 보낸 수도 `5`로 읽힌다). 수(`number`)는 `double precision`이라 소수 글자는 가장 짧은 십진 글(`"1.10"`은 `"1.1"`)로 돌아온다. 수량은 `numeric`이라 소수점 아래 자릿수를 지킨다. 같은 수가 다른 글로 와도 같은 값으로 견주므로(결정 11) 다시 써지지 않는다. 정확히 담을 수 없는 입력(2^53을 넘는 정수, 17자리를 넘는 소수)은 관문이 거절한다(결정 12). 필요하면 수의 열을 `numeric`으로 바꾸는 것이 길이지만 질의 스키마 설계(`storage-shape.md` 6절)가 `double precision`을 말해서 바꾸지 않았다.

**인덱스와 `EXPLAIN`**(`TestTixTheNameIndexesAnswerTheExactNameAndTheStartOfTheNameWithoutCase`, 3만 행, `ANALYZE` 뒤, 정렬 규칙 `C.UTF-8`):

| 질의 | 계획 |
|---|---|
| `WHERE name = 'srv-12345'` | `Index Scan using t_45_name` (`name = 'srv-12345'`) |
| `WHERE lower(name) LIKE 'srv-123%'` | `Index Scan using t_45_name_ci` (`lower(name) ~>=~ 'srv-123' AND lower(name) ~<~ 'srv-124'`) |
| `WHERE lower(name) LIKE lower('SRV-123') \|\| '%'` | 같음 |
| `WHERE lower(name) = lower('SRV-12345')` | `Index Scan using t_45_name_ci` (`lower(name) = 'srv-12345'`) |
| `WHERE ref = '...'`(타입 테이블) | `Seq Scan`(의도한 것: `ref`는 등록부가 찾는다. 등록부의 같은 질의는 `object_ref`를 쓴다) |

값이 매개변수일 때(`PREPARE`, `plan_cache_mode = force_custom_plan`)도 `t_45_name_ci`를 쓴다. 일반 계획(generic plan)은 값을 모르므로 접두어 `LIKE`를 인덱스에 태우지 못한다. 질의 스키마를 만드는 쪽은 값이 상수로 박힌 계획이나 사용자 질의의 사용자 지정 계획에 기댄다.

**첫 적재의 시간**(인덱스가 쓰기에 주는 비용. 개발 적재가 행 단위 길로 합성 세계를 처음 넣는 시간. 같은 세계를 이름 인덱스 336개를 지운 KG와 둔 KG에 넣었고, 기계는 코어 2개를 다른 일과 나눠 써서 잡음이 크다):

| 세계 | 인덱스 없음 | 인덱스 있음 | 반복 |
|---|---|---|---|
| 규모 500(객체 598개) | 1.41초(1.43, 1.47, 1.38, 1.37) | 1.44초(1.43, 1.58, 1.35, 1.40) | 네 번 |
| 규모 30000(객체 10,564개) | 31.1초(31.9, 30.3) | 34.2초(34.1, 34.3) | 두 번 |

작은 세계에서는 잡음 안이고, 객체가 1만 개 넘는 세계에서 약 10% 느리다. 객체가 몰리는 타입 몇 개의 이름 인덱스 둘이 행마다 쓰기를 더하는 값이다. 3천만 객체의 값은 측정하지 않았고 더 큰 기계에서 같은 도구로 잰다.

**시각의 열 비교**: 변동 값을 열로 옮기는 단계는 SQL 하나라서 행이 많으면 한 트랜잭션이 길어진다(상태 테이블 전체를 한 번 고친다). 이 길을 지나는 KG는 알파의 개발 KG뿐이다(새 저장은 아직 릴리스에 나가지 않았다). 큰 KG의 맞추기를 구간으로 나누는 것은 필요해질 때 한다.

## 이유

질의 스키마의 뷰가 변동 값을 `prov`의 JSON에서 꺼내려면 매 행마다 JSON을 열고 형 변환을 해야 하고, `oper_state = 'down'` 같은 "지금 내려간 것 전부" 질의가 인덱스를 타지 못한다. 값 열은 그 질의의 인덱스를 쓰고 변동 갱신 비용의 모형(제자리 갱신, 채움 비율 70)도 이미 값 열 기준으로 잰 것이다. 값이 두 곳에 있으면 어느 쪽이 진실인지 정해야 하고 두 쪽을 맞추는 비용이 쓰기에 붙는다. 시각 키는 뷰의 `provenance` 열이 문서의 항목과 같은 모양을 내려면 하나여야 한다. 이름 인덱스가 없으면 3천만 객체에서 가장 큰 타입(약 320만 행)의 이름 검색이 초 단위다. 시각 형식을 쓰는 쪽에서 검사하지 않으면 뷰가 `praxium_ts`로 읽을 때 오류가 난다(`storage-shape.md` 6절의 경고).

## 대안과 기각 사유

- **값을 `prov`에 두고 뷰가 JSON에서 읽는다**: 변동 값의 인덱스와 제자리 갱신 모형이 맞지 않고 값 열과 인덱스를 지워야 한다. 기각(설계 책임자의 결정).
- **값을 열과 `prov`에 둘 다 둔다**(옛 결정): 같은 값이 두 곳에 있다. 기각.
- **뷰가 `prov`의 시각 키 `s`를 `t`로 바꿔 준다**: 새 저장은 릴리스에 나가지 않아 바꿀 수 있고, 뷰의 식이 단순해진다. 기각(키를 맞췄다).
- **수의 열을 `numeric`으로**: 소수 글자가 모양 그대로 돌아오지만 질의 스키마 설계의 열 형과 어긋난다. 이번에는 하지 않고 결과에 적었다.
- **`ref` 인덱스를 `t_`에도 둔다**: 등록부가 같은 일을 하고 쓰기 비용이 는다. 기각.
- **이름 인덱스 하나(`lower(name)`)만 둔다**: `name = $1` 질의가 인덱스를 못 탄다. 기각.
- **큰 KG를 위해 값 옮기기를 구간으로 나눈다**: 지금 지나는 KG가 알파의 개발 KG뿐이라 하지 않는다.

## 결과

- 질의 스키마의 뷰(B3-1)는 변동 값을 값 열에서, 시각을 `praxium_ts`로, 부모 타입을 자식 뷰의 `UNION ALL`로 읽을 수 있다.
- 읽는 쪽(`kgread_misc.go`의 `volatileOf`)은 값을 열에서 읽고 `prov`에서 등급, 근거, 시각, 출처를 읽는다. 값이 열에 맞지 않으면 쓰기가 오류를 낸다.
- 옛 KG(생성기 버전 4 이하)는 서버가 시작할 때 맞춘다. 열로 바꿀 수 없는 옛 값이 있는 KG는 맞춰지지 않고 이유와 함께 거절된다.
- 시각 형식이 좁아져서 오프셋이 있는 시각은 수집의 셀 변환이 UTC로 바꿔 저장에 보낸다(결정 10). 수집을 거치지 않고 적용기에 바로 오는 다른 모양의 시각은 그 레코드가 문제로 가고 저장되지 않는다.
- `stage_volatile.entry`의 시각 키가 `t`로 바뀌었다. 이 테이블을 채우는 코드가 만들어질 때 `t`를 써야 한다.
- 잠긴 테스트가 바뀐 것은 이 ADR의 `amends_tests`에 있다: 버전 단언(`TestControlReservesThePartitionPrefix` 4에서 5), 새 타입의 단계 목록(`TestDiffFollowsAdditions`에 이름 인덱스 둘), 이전 버전의 단계만 보던 테스트(`TestIngestHeldDiffFromGeneratorThree...`는 다음 버전을 4로 고정해 같은 단언을 그대로 하고 5의 단계는 새 테스트이 본다), 그리고 값을 직접 심는 시드 한 곳(`v2rSeed`)이 값 열에 값을 넣게 바뀌어 지문이 바뀐 테스트들(단언은 그대로).
- 독립 검토의 낮은 항목 중 맞추기가 상태 테이블 전체를 한 트랜잭션으로 고치는 것(문장 제한 시간에 걸리면 그 KG는 매번 실패하고 거절된 채 남는다), 변동 속성 N개가 같은 행을 N번 고치는 것(채움 비율 70의 여유), 이름 인덱스의 쓰임(타입 없이 이름으로 찾는 `object` 뷰가 읽을 등록부 `object.name`에 인덱스가 없는 것, 기본 정렬 규칙에서 `name LIKE` 접두어가 인덱스를 못 타는 것, `ILIKE`와 가운데 일치)은 이번에 바꾸지 않았다. 질의 스키마의 구현(B3-1)과 큰 KG가 생길 때 다룬다.
