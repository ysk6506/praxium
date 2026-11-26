---
title: ADR-0117 HTTP 소스 자식 데이터셋의 완결 판정 보완 — 빈 응답 가드, 미리 보기와 점검의 부모 요청 상한, 키 없는 상세와 다시 보내기의 문구, 보내지 않는 부모 값 규칙
kind: decision
status: active
enforced_by:
  - TLOCK001
  - GOCOV001
  - backend/internal/httpsource/hxe_fix_test.go
touches_protected:
  - docs/product-specs/requirements-ingestion.md
  - docs/design-docs/adr/0111-http-source-child-datasets-values-of-a-parent-in-the-request.md
updates_docs:
  - docs/design-docs/sources-and-adapters.md
  - docs/product-specs/requirements-ingestion.md
amends:
  - adr: "0111"
    what: 빈 응답 가드(6항), 미리 보기와 점검의 부모 요청 상한(8항), 키 없는 상세의 점검 문구와 다시 보내기의 직전 오류(7항과 8항), 보내지 않는 부모 값 규칙(4항)과 완결 분모의 문장
amends_tests:
  - go:backend/internal/httpsource::TestHxeABatchOfValuesThatJSONWritesLongerFitsTheBody
  - go:backend/internal/httpsource::TestHxeADetailThatHasNoKeyIsAValueThatWasNotRead
  - go:backend/internal/httpsource::TestTheRequestIsMadeInOnePlaceWithTheTwoMethodsAndNoOtherNetworkCodeIsImported
  - go:backend/internal/httpsource::TestTheValuesOfASecretAreTakenOutInOneFileAndNothingIsLoggedOrPrinted
test_change_reason: 단언을 바꾼 것은 둘이다 — TestHxeADetailThatHasNoKeyIsAValueThatWasNotRead는 목록 꼴 오류 원소 세 행을 더하고(기대 seen), 모든 값에 빈 목록이 온 행의 기대를 complete에서 seen으로 바꾼다(R2 빈 응답 가드, 이 ADR의 결정 1). TestHxeABatchOfValuesThatJSONWritesLongerFitsTheBody는 본문 한도를 1000에서 4000 바이트로, 값을 10개에서 40개로 바꿔 3배 추정이 통과하지 않게 한다(단언은 그대로). TestTheRequestIsMadeInOnePlaceWithTheTwoMethodsAndNoOtherNetworkCodeIsImported와 TestTheValuesOfASecretAreTakenOutInOneFileAndNothingIsLoggedOrPrinted는 단언과 금지 목록이 그대로이고, httpsource의 제품 코드를 글자로 읽으므로 child.go, parentscan.go, readloop.go, send.go의 변경으로 지문만 바뀌었다 (새 코드에 금지된 글자는 없다).
decision_hash: 4e0470ba79b9af27
---
# ADR-0117 HTTP 소스 자식 데이터셋의 완결 판정 보완

## 맥락

ADR-0111(#104)이 자식 데이터셋의 두 단계 읽기, 건너뜀의 규칙, 한도를 정했다. 그 뒤 독립 재확인(R1부터 R6)이 네 가지를 찾았다. 모든 값의 응답이 레코드 없이 비어 있으면 자식이 완전으로 끝나 지워짐 길이 열린다(R2). 미리 보기와 점검이 부모를 끝까지 읽어 소스에 요청을 많이 보낸다(R3). 키 없는 첫 상세가 점검에서 성공으로 보이고, 막힌 다시 보내기가 원인을 가린다(R4). 앞뒤 공백과 `;` 앞이 `.`·`..`인 부모 값이 문서의 "보내지 않는 값" 목록에 없다(R6). 이 ADR은 그 보완을 결정으로 적는다. ADR-0111은 main에서 받아들인 결정이라 고치지 않는다. 바뀌는 문장은 아래 표에 적는다.

## 결정

1. **빈 응답 가드(R2).** 자식의 서로 다른 부모 값(규칙에 걸린 값 포함)이 셋을 넘는데 어느 응답에도 레코드가 하나도 없으면, 그 데이터셋은 보인 것만이다(사유: 모든 응답이 비었으면 권한이나 경로가 잘못된 것일 수 있다). 일부 값만 비었으면 지금처럼 완전이다. 최상위 데이터셋의 규칙은 바꾸지 않는다.
2. **미리 보기와 점검의 부모 요청 상한(R3).** 미리 보기와 점검은 부모를 읽는 요청을 스무 건(`previewParentRequests`)까지만 보낸다. 그때까지 모은 값으로 자식을 읽고, 값이 하나도 없으면 "the first 20 requests of the parent gave no value"라고 알리며 자식 요청은 0건이다.
3. **키 없는 상세의 점검(R4).** 점검의 첫 값 상세가 응답 객체 하나이고 자연 키가 없어 행이 못 되면, 점검은 설정 문제(`config`)로 알린다(첫 값의 상세가 키를 주지 않는다는 문구).
4. **다시 보내기의 문구(R4).** 점검이나 미리 보기의 다시 보내기가 요청 상한에 막히면, 문구에 직전 응답의 실패(예: 503)를 붙인다. 막힌 원인이 한도만이 아니라 직전 실패라는 것이 드러난다.
5. **보내지 않는 부모 값(R6).** 앞뒤에 공백이 있는 값과 `;` 앞이 `.`이나 `..`인 값은 보내지 않고 `source.parent_value_invalid`로 센다. 공백이 흔한 소스는 값의 5%를 넘어 자식이 불완전이 될 수 있다. 안전한 쪽의 변화다.

## 고친 것 (ADR-0111의 바뀌는 문장)

| ADR-0111 위치 | 바뀌는 문장 | 이 ADR의 결정 |
|---|---|---|
| 4항 (경로의 인코딩) | 보내지 않는 값 목록 | 앞뒤 공백, `;` 앞이 `.`·`..` 추가 (결정 5) |
| 6항 (완결성) | 값마다 빈 배열은 정상 | 값이 셋을 넘고 레코드가 하나도 없으면 보인 것만 (결정 1) |
| 6항 (완결성) | 분모 문장 | 규칙에 걸린 값은 레코드마다, 묶음의 객체 응답은 단위마다 센다. 결과는 늘 보인 것만 이상 |
| 7항 (한도) | 점검과 미리 보기의 부모 읽기 | 부모 요청 스무 건 상한 (결정 2) |
| 8항 (점검과 미리 보기) | 부모 읽기 | 스무 건까지, 값이 없으면 알림 (결정 2) |
| 8항 (점검과 미리 보기) | 점검의 키 없는 상세 | 설정 문제로 알림 (결정 3) |
| 8항 (점검과 미리 보기) | 막힌 다시 보내기 | 직전 실패를 붙임 (결정 4) |

## 잠긴 테스트의 변경

- `TestHxeADetailThatHasNoKeyIsAValueThatWasNotRead`와 `TestHxeABatchOfValuesThatJSONWritesLongerFitsTheBody`는 단언을 바꿨다. 이유는 `test_change_reason`에 건별로 적었다.
- `TestTheRequestIsMadeInOnePlace...`와 `TestTheValuesOfASecret...`는 단언이 그대로이고 지문만 바뀌었다.

## 이유

- 빈 응답 가드는 지워짐 길을 닫는다. 값이 많으면 빈 응답 하나가 곱해지므로, 권한이 사흘 이어 잘못되면 데이터셋이 늙는 대신 보인 것만으로 남는다.
- 스무 건 상한은 값이 두 개만 번갈아 오거나 끝없는 부모에서 요청이 2,000건까지 가는 것을 막는다.

## 남는 위험

- 모든 값의 응답이 빈 성공이면 보인 것만이다. 최상위 데이터셋은 같은 응답을 완전으로 읽으므로(`{"items":null}`, 204, `[]`), 큰 지워짐은 확인 뒤 적용(I-8)의 안전장치가 맡는다.
- 앞뒤 공백이 흔한 부모 값은 보내지 않으므로 자식이 불완전이 될 수 있다.
- 값이 셋을 넘고 모든 응답이 비면 보인 것만으로 끝내므로, 건강할 때 늘 비는 자식(예: 호스트별 경보)은 옛 행이 지워지지 않는다. 정의로 끄는 스위치는 후속(1차 클로즈 베타 전).
