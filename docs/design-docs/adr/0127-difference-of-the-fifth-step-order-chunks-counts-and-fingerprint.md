---
title: ADR-0127 새 수집 흐름 5단계의 뒷부분 — 차이, 순서와 청크, 타입별 집계, 변경 지문
kind: decision
status: active
enforced_by:
  - TLOCK001
  - GOCOV001
  - backend/internal/ingest/dfi_run_test.go
  - backend/internal/pgingest/dfi_edge_test.go
  - backend/internal/pgingest/dfi_flow_test.go
  - backend/internal/pgingest/dfi_parity_test.go
  - backend/internal/pgingest/dfi_props_test.go
  - backend/internal/pgingest/dfi_card_test.go
  - backend/internal/pgingest/dfi_resume_test.go
  - backend/internal/pgingest/dfi_scale_test.go
  - backend/internal/pgingest/dfi_damage_test.go
  - backend/internal/pgschema/dfi_generation_test.go
touches_protected:
  - harness/config/test-lock.json
  - docs/product-specs/requirements-ingestion.md
updates_docs:
  - docs/product-specs/requirements-ingestion.md
  - docs/design-docs/ingestion-at-scale.md
  - docs/design-docs/ingestion-at-scale-identity.md
amends_tests:
  - go:backend/internal/pgschema::TestControlReservesThePartitionPrefix
test_change_reason: 단언을 줄이거나 임계값을 넓힌 것은 없다. TestControlReservesThePartitionPrefix는 생성기 버전의 숫자를 단언하므로 13에서 14로 바꾼다(이 결정 7). 파티션 접두어를 예약한다는 단언은 그대로다.
decision_hash: d78d8e61dd140f03
---
# ADR-0127 새 수집 흐름 5단계의 뒷부분 — 차이

## 맥락

4단계(ADR-0115, ADR-0119, ADR-0122)가 객체와 링크를 `stage_object`, `stage_link`, `stage_ident`에 두고, 5단계의 앞부분인 격리(ADR-0123)가 KG가 가질 수 없는 줄을 그 표에서 뺐다(표시가 아니라 뺌). 남은 일은 이것을 KG와 견줘 적용(ADR-0099의 `StagedApplier`)이 읽는 `ingest_diff`를 만들고, 순서와 청크 번호를 저장하고, 타입별 집계와 변경 지문을 내는 것이다([수집의 규모 설계](../ingestion-at-scale.md) 4절 5단계, 5.2절). 적용이 기대하는 것은 이렇다(`pgstore/kgstaged*.go`): 청크 번호는 0부터 틈이 없고, 줄이 없는 청크는 거절하고, 종류의 번호가 쓰는 순서이며(객체, 키, 링크, 식별자), 바꿈과 닫힘 줄의 옛 해시는 KG에 열린 행의 내용 해시와 같아야 하고, 해시가 없는 자리는 NULL이다. 식별자 줄과 키 줄은 해시(`IdentRowHash`, `KeyRowHash`)로 대상을 가리킨다.

## 결정

1. **새 포트를 둔다.** `ingest.Differ`(`BeginDiff`, `DiffPlace`, `DiffStep`, `DiffReport`)이고 `RunDiff`가 몬다. Go가 결정할 것이 없어 SQL만이다(ADR-0114의 확장과 같은 모양). `calc_job`은 6(`CalcJobDiff`)이고 이어 갈 위치는 실행 기록의 카운트 `diff`에 둔다. 격리의 세 국면이 모두 끝난 실행에서만 시작한다. 국면은 `rows`(줄), `plan`(계획), `order`(순서), `print`(지문)이고 한 번씩이며, 단위 하나는 쓰기와 위치 이동이 한 트랜잭션이다. 두 번 시작, 시작 전과 끝난 뒤의 단위, 다른 실행의 계산 표는 `CodeRunStage`, 임대를 잃은 서버는 읽기도 `CodeRunLeaseLost`다.
2. **줄(`rows`)은 원천마다 키 범위로 읽는다.** 원천은 순서대로 이번 `stage_object`(객체와 그 식별자), `calc_object`(퇴역하는 객체와 그 링크), `stage_link`, `calc_record`(이번 실행의 레코드가 만든 KG 링크), `calc_isolated`(격리가 뺀 링크), `calc_newkey`(키)다. 단위는 원천의 키 `DefaultDiffRows`개이고, 한 줄마다 KG가 가진 것을 인덱스로 한 행씩 읽는다(스칼라 부분 질의나 `OFFSET 0`의 측면 부분 질의, ADR-0123 결정 7과 같다). 다른 것만 `calc_diffrow`에 쓰고, 문장이 돌려준 (종류, 타입, 변화)를 단위가 센다. 무엇을 어느 표와 견주는지는 아래 표다.
3. **KG 링크를 닫는 조건은 격리의 "남는 KG 링크"의 정확한 반대이고 SQL 조각 하나(`pgingest/isolate_sql.go`의 `isoKGLinkStays`)로 쓴다.** 차수 검사의 `isoFixedSQL`과 차이의 `diffCloseLinks`가 같은 함수를 부른다. 이번 실행이 다시 만들지 않은(`stage_link`에 없는) KG 링크는 남지 않으면 닫는다. 남는다는 것은 격리가 빼지 않았고, 끝이 이번에 퇴역하지 않으며, 이번 실행이 읽지 않는 레코드의 해결된 From 참조가 있거나 해결된 From 참조가 아예 없는 것이다. 닫힐 수 있는 KG 링크는 퇴역하는 객체에 닿은 것, 이번 실행의 레코드가 만든 것, 격리가 뺀 것뿐이라(남지 않는 조건의 세 갈래) 세 원천이 그것만 읽는다. 한 링크를 둘 이상의 원천이 찾으면 한 번만 쓴다.
4. **순서와 청크.** 줄의 순서는 (종류, 타입 번호, `ref`의 바이트 순서(`COLLATE "C"`), 같은 `ref` 안에서는 `tie`)다. `tie`는 객체와 링크는 빈 값, 키는 `KeyRowHash`, 식별자는 `IdentRowHash`다(한 객체가 키나 식별자를 여럿 가지므로). 계획(`plan`, 단위 하나)은 센 수에서 (종류, 타입)마다 줄 수, 앞의 순서 번호, 첫 청크를 `calc_diffplan`에 쓴다. 순서(`order`)는 (종류, 타입)마다 `calc_diffrow`를 키 범위로 `DefaultDiffRows`개씩 읽어 `ord = 앞의 번호 + 이미 매긴 수 + row_number()`, `chunk_no = 첫 청크 + (그 타입 안의 자리 - 1) / 청크 한도`로 `ingest_diff`에 쓴다. 순서는 1부터, 청크는 0부터 틈이 없고, 청크는 타입 경계에서 자르고 줄 수 한도(`DefaultDiffChunkRows`, 1만)로도 자른다. 한 청크는 한 종류, 한 타입이다. 매긴 수가 계획의 수와 다르면 단위가 실패한다.
5. **타입별 집계.** (종류, 타입, 변화)마다 줄 수를 실행 기록의 카운트(`sync_run.counts`의 `diff.tally`, 이미 있는 JSON. 표를 바꾸지 않는다)와 `RunDiff`의 보고(`DiffCounts.Tally`)에 둔다. 격리 뒤의 수이고 저장된 줄과 같다. 대량 소실의 판단(REQ-118, 멈춤)은 I-7이다.
6. **변경 지문은 새 해시 규칙을 만들지 않는다.** 지문(`print`)의 단위는 청크 하나이고, 적용과 같은 문장(`pgschema.ChunkDigestSQL`)으로 청크의 다이제스트를 만들어 `ChainDigest`로 잇는다. 마지막 청크 뒤의 사슬이 지문이고, 적용이 끝에 내는 사슬(`StagedProgress.Chain`)과 같다. 줄이 없으면 청크도 없고(`StagedEmpty`) 지문은 사슬의 시작인 32바이트의 0이다. 지문은 실행 기록의 `change_fingerprint`(마이그레이션 0006에 있던 열)와 보고에 둔다.
7. **생성기를 14로 올린다.** 표 `calc_diffrow`(키 (종류, 타입, `ref`, `tie`))와 `calc_diffplan`(키 (종류, 타입))만 더하고 인덱스는 더하지 않는다(키로 읽는다). 계산 표라 `ComputeTables()`에 들어 실행이 끝나면 비워지고 KG 삭제(ADR-0125)도 따라온다. 단계는 `to_regclass`로 지키고 두 번 해도 같다. 이미 있는 표는 바꾸지 않는다(REQ-680).
8. **퇴역하는 객체의 식별자는 닫지 않는다.** 옛 흐름이 그렇다(`ingest/apply.go`의 `staleIdentifiers`는 남는 객체만 본다). 격리 ①도 퇴역하는 객체를 식별자의 주인으로 보지 않으므로 다른 객체가 그 식별자를 받는다.

## 종류마다 견주는 표

| 종류 | 원천(이번 실행) | 견주는 KG의 표 | 새로 생김 | 바뀜 | 닫힘 |
|---|---|---|---|---|---|
| 객체 | `stage_object` | 그 타입의 테이블(열린 행, `obj_no`) | 열린 행이 없음(새 객체, 퇴역했다 돌아온 객체) | 열린 행의 `content_hash`가 다름 | — |
| 객체 | `calc_object`의 `why = 4`이고 `stage_object`에 없음 | 등록부(`object`, 타입)와 그 타입의 테이블 | — | — | 열린 행이 있음(옛 해시는 그 행의 것) |
| 키 | `calc_newkey` | `object_key` (`type_no`, `ctx_no`, `sig_hash`) | 색인에 없음(`ctx_no`, `sig_hash`, `KeyRowHash`를 함께) | — | — |
| 링크 | `stage_link` | `link`(열린 행, `link_no`) | 열린 행이 없음 | `content_hash`가 다름 | — |
| 링크 | 퇴역하는 객체에 닿은 KG 링크(`link_src`, `link_dst`), 이번 레코드의 해결된 From 참조(`record_ref`), `calc_isolated`의 링크 | `link`, `stage_link`, 결정 3의 조건 | — | — | `stage_link`에 없고 남지 않음 |
| 식별자 | `stage_object`의 객체마다 `stage_ident` | `object_identifier`(그 객체의 열린 줄) | `stage_ident`에만 있음 | — | 열린 줄에만 있음 |

## 예외 상황

| 상황 | 결과 | 테스트 |
|---|---|---|
| 바뀐 것이 없음 | 줄 0, 청크 0, `StagedEmpty`, 지문은 32바이트의 0 | `TestDfiARunThatChangesNothingHasNoRowNoChunkTheFingerprintOfNothingAndAnEmptyApply`, `TestDfiAFirstRunAddsWhatItStagedAndTheApplyMakesTheKGTheHeadSays` |
| 한 타입의 줄이 청크 한도보다 많음 | 그 타입이 여러 청크, 경계는 `ref` 순서 | `TestDfiATypeOfMoreRowsThanAChunkIsCutInTheOrderOfTheIdsAndAChunkHasOneKindAndOneType` |
| 객체의 타입이 바뀜(같은 자연 키, 다른 `ref`) | 옛 것은 옛 타입의 청크에서 닫힘(링크와 함께), 새 것은 새 타입의 청크에서 생김 | `TestDfiAnObjectWhoseTypeChangesIsRetiredInTheChunkOfItsOldTypeAndAddedInTheChunkOfItsNew` |
| 퇴역 후보인데 다시 기여가 생김 | 4단계가 정한다: 식별이 묶은 객체는 `why = 1`이고(한 객체는 `calc_object`에 한 번) 4단계는 `why = 4`를 계산하지 않는다. 차이는 `stage_object`에 있으면 견주고 없으면 닫는다 | `TestDfiEveryChangeOfEverySourceIsARowAndWhatIsTheSameIsNone`(돌아온 객체, 이미 퇴역한 후보), `TestDfiTheKGTheStagedApplyWritesIsTheOneTheOldPipelineMakesFromTheSameCaptures` |
| 해시가 없는 자리 | NULL(빈 바이트가 아님). SQL 다이제스트가 Go 기준 구현(`InputDigest`)과 같음 | 모든 흐름 테스트의 `dfiShape` |
| 격리된 링크·식별자, 되돌린 새 객체 | 줄이 없음. 격리된 KG 링크는 닫힘 | `TestDfiWhatTheIsolationTookOutHasNoRowAndANewObjectTakenBackHasNone` |
| 두 소스의 레코드가 받치는 객체 | `stage_object`의 키가 객체라 줄 하나. `dfiShape`가 한 객체, 링크, 키, 식별자의 줄이 둘이 아님을 본다 | `dfiShape` |
| 이어받은 서버가 단위 가운데에서 이어 감 | 같은 줄, 같은 보고 | `TestDfiAServerThatTakesTheRunOverAtAnyUnitEndsWhereOneThatDidNotStopEnds` |

## 옛 흐름과 다른 점

같은 수집 사례(서버의 위치와 랙이 바뀌고, 랙과 가상 머신이 사라져 세 번 뒤 닫히며 링크와 함께 퇴역)를 옛 `Runner`(행 단위 적용)와 새 흐름(`StagedApplier`)에 주면 실행마다 두 KG의 상태 해시가 같고 새 KG는 `Verify`를 지난다(`TestDfiTheKGTheStagedApplyWritesIsTheOneTheOldPipelineMakesFromTheSameCaptures`). 차이에서 새로 생긴 다른 점은 없다. 차수에서 KG에 있는 링크가 `:1` 끝을 지키는 것과 결합마다 세는 것은 ADR-0123의 표에 있고, 이 사례는 그 경우를 피한다.

- 변화가 없는 실행은 옛 흐름에서 머리를 쓰고 새 흐름에서는 적용하지 않는다(ADR-0099 결정 5). 상태 해시는 같다.
- 변경 지문은 옛 흐름에 없다.

## 이유

- 퇴역하는 객체, 닫는 KG 링크, 키, 식별자의 줄은 `stage_*`가 아닌 곳에서 오고 그 표에는 (타입, `ref`)의 인덱스가 없다. (종류, 타입) 안을 `ref` 순서로 범위만 읽으려면 줄을 먼저 그 순서의 키를 가진 표에 모아야 한다. 그러지 않으면 버킷마다 원천 전체를 읽어 단위 수 × 원천의 제곱이 된다. 그래서 `calc_diffrow`를 둔다. 계획은 센 수에서 나와 원천을 두 번 읽지 않는다.
- 순서를 `ref`와 해시로 정하면 번호를 받은 순서, 페이지, 단위의 크기에 매이지 않는다(번호는 시퀀스라 실행마다 다르다). 결정 9(I-5 제안서)의 `ref` 순서를 그대로 썼다.
- 남는 KG 링크의 조건을 한 곳에 두면 차수 검사가 남는다고 본 링크를 차이가 닫거나 그 반대가 될 수 없다. 그렇게 되면 KG의 한 `:1` 끝에 링크가 둘이 되거나 자리가 빈다.
- 지문을 적용의 사슬과 같게 하면 확인 뒤 적용(6단계)이 같은 규칙 하나로 견준다(새 해시 규칙을 만들지 않는다).

## 대안과 기각 사유

- **계획대로 `stage_object`와 `stage_link`에 (타입, `ref`) 인덱스를 두고 버킷이 그것만 읽는다.** 폐기 줄과 키, 식별자 줄의 순서를 정할 수 없다(위의 이유). 기각.
- **계획 단위가 원천을 다 읽어 (종류, 타입)마다 센 뒤 버킷이 다시 계산한다.** 원천을 두 번 읽고, 폐기 줄의 순서 문제는 그대로다. 기각.
- **`ingest_diff`에 임시 번호로 넣었다가 고친다.** I3(준비 테이블은 넣기만)을 어기고, 적용이 읽는 표에 덜 된 줄이 생긴다. 기각.
- **번호가 없는 줄(`ref`를 쓰는 줄)로 지문을 만든다.** 새 해시 규칙이다. 이번에는 하지 않고 아래 한계에 넘긴다.

## 알려진 한계와 넘김

- **지문은 객체와 링크의 번호에 매인다.** ADR-0095의 줄에 번호(`entity_no`)가 들어 있어서, 새 객체와 새 링크가 번호를 받은 순서(행의 순서, 시퀀스의 값)가 다르면 같은 변화라도 지문이 다르다. 줄, 청크 경계, 순서는 같다. 테스트는 이것을 그대로 고정한다: 같은 번호면 같은 지문이고, 한 번호 매김을 다른 번호 매김으로 바꿔 넣으면 다른 쪽의 지문이 된다(`TestDfiTheRowsAndTheChunksHangNeitherOnTheNumbersNorOnTheSizeOfAUnitAndTheFingerprintOnTheNumbersAlone`). 그래서 6단계의 "다시 계산해 지문이 같을 때만 적용"(ADR-0086 결정 16)은 이 지문으로는 새 객체가 있는 변화에서 늘 다르다(다시 계산하면 시퀀스가 새 번호를 준다). 6단계를 짓는 결정이 둘 중 하나를 정한다: 번호 대신 `ref`를 담은 줄로 지문을 만드는 새 규칙, 또는 다시 계산할 때 번호를 다시 쓰는 규칙.
- 한 KG 링크를 이번 실행의 레코드 m개가 함께 만들었고 이번에 다시 만들지 않으면, 그 레코드가 든 단위마다 그 링크의 From 참조를 읽는다(단위 수 × m). 한 링크를 많은 레코드가 만드는 경우는 드물어 받아들인다.
- 퇴역하는 객체의 링크는 그 객체가 든 단위 하나가 모두 읽는다(링크 m개면 m에 비례, `TestDfiAnObjectThatRetiresWithMLinksIsReadWithRowsThatGoWithM`). 객체 하나의 링크를 단위로 자르지 않는다.
- I1과 I2는 `pgingest`의 테스트다(I-5 제안서 결정 15). 4·5단계가 내는 문제의 같음은 정리(I-7)가 없어 테스트가 실행마다 레코드의 문제를 갈아 끼워 견준다. 수신과 식별이 내는 문제는 전체 계산이 다시 내지 않으므로 견주지 않는다.
- 적용 뒤의 정리(레코드 참조, 문제, 원본 레코드)는 I-7이고 테스트는 테스트의 정리로 대신한다.

## 잠긴 테스트의 변경

단언을 줄이거나 임계값을 넓힌 것은 없다. `TestControlReservesThePartitionPrefix`의 생성기 버전 숫자만 13에서 14로 바뀐다.

## 결과

- `ingest_diff`는 `StagedApplier`가 그대로 적용하는 모양이고, 적용 뒤 KG는 머리가 말하는 상태다(`Verify`). 순서는 행의 순서, 페이지, 단위, 데이터셋의 순서, 번호를 받은 순서에 매이지 않고, 단위가 읽는 행은 그 범위에 비례한다.
- 새 요구 REQ-960부터 REQ-970은 `requirements-ingestion.md`에 있다. 고친 문서는 `ingestion-at-scale.md`(상태, 5단계, 5.2절의 계산 표)와 `ingestion-at-scale-identity.md`(4절, 7절)다.
