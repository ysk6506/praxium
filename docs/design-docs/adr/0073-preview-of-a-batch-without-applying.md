---
title: ADR-0073 적용하지 않고 차이를 내는 길
kind: decision
status: active
enforced_by:
  - backend/internal/graphcheck/preview_test.go
  - backend/internal/pgstore/preview_test.go
  - backend/internal/filejournal/preview_test.go
  - TRACE006
updates_docs:
  - docs/design-docs/kg-storage-apply.md
  - docs/design-docs/kg-storage.md
  - docs/product-specs/requirements-model-storage.md
touches_protected:
  - docs/product-specs/requirements-model-storage.md
  - harness/config/test-lock.json
amends: []
amends_tests: []
decision_hash: f81e6b4a70358c73
---
# ADR-0073 적용하지 않고 차이를 내는 길

## 맥락

배치를 적용하기 전에 "무엇이 새로 생기고, 바뀌고, 닫히는지"를 사람에게 보이고 확인받으려 한다. 지금은 차이를 적용기가 쓰면서 찾으므로, 쓰지 않고 차이만 볼 길이 없다. 수집, REST, 화면에 닿는 일은 이 결정에 넣지 않고 `core.graph`에 길을 더하는 데까지만 한다.

## 결정

1. **`Applier.Preview(ctx, kg, batch) (ChangePreview, error)`.** 입력은 적용과 같은 배치와 KG이다. 쓰지 않는다: 헤드, 임대, 변경 번호, 버전이 그대로이고 임대를 잡지 않는다.
2. **무엇을 내는가.** 바뀌는 객체와 링크의 목록(`ChangeItem`: 종류, ID, 타입 ID, 하는 일(`added`, `changed`, `retired`), 옛 해시, 새 해시), 요약(후보 수, 바뀌는 수, 그대로인 수, 종류별·타입별·하는 일별 개수), 비교한 상태의 변경 번호, 지문이다. 목록은 적용기가 일하는 차례(객체가 먼저, ID 순)다.
3. **같은 검사.** 배치의 검증과 상태 검사(링크의 끝, 카디널리티, 폐기하는 객체에 남은 링크, 휘발 값, 식별자)는 적용이 쓰는 코드를 그대로 쓴다. 적용이 거절할 배치는 같은 코드와 같은 문장으로 거절한다. 반쯤 된 적용이 헤드에 있으면 계산하지 않고 `graph_apply_in_progress`를 낸다(같은 `apply_id`여도 이어 가지 않는다).
4. **적용 코드와 함께 쓰는 부분.** 후보를 스냅숏과 견주어 바뀌는 것을 모으는 부분(`chunkBuilder`)과 적용 전에 하는 상태 검사(`checkFresh`)를 적용과 미리 보기가 같이 쓴다. 판단이 두 벌이 아니다. 적용의 동작은 바뀌지 않는다.
5. **지문.** `(종류, ID, 하는 일, 옛 해시, 새 해시)`의 목록을 차례대로 정규 인코딩해 만든 내용 해시다(`praxium.preview.v1`). 타입 ID는 ID와 해시에 담겨 있어 넣지 않는다. 값의 시각, 소스 참조, 적용 ID, 배치 안의 항목 순서, 그대로인 항목은 들어가지 않는다(내용 해시에 없거나 목록에 없다). 같은 배치와 같은 KG 상태면 언제나 같다.
6. **목록에 상한을 두지 않는다.** 배치는 이미 메모리에 있고 목록은 그 크기를 넘지 않는다. 지문은 전체가 필요하다. 보이는 쪽이 목록을 잘라 보일 수 있고, 요약은 언제나 전부를 센다.
7. **휘발 값, 식별자, 식별 이벤트는 목록에 없다.** 버전이 있는 행이 아니므로 "바뀌는 것"의 목록에 넣지 않는다. 검사는 한다.

## 이유

지문이 목록만으로 정해지면, 사람에게 보인 차이와 적용하려는 차이가 같은지를 해시 하나로 견줄 수 있다. 적용과 같은 코드를 쓰면 미리 본 것과 실제로 바뀌는 것이 어긋날 길이 없고, 테스트는 임의의 상태와 배치에서 그 일치를 확인한다.

## 대안과 기각 사유

- 미리 보기를 따로 구현하기: 판단이 두 벌이 되어 어긋난다.
- 적용을 드라이 런으로 하고 리버트하기: 헤드와 임대를 건드리고 저장소마다 되돌리는 방법이 다르다.
- 요약만 내기: 사람이 무엇이 바뀌는지 볼 수 없고 지문을 만들 재료가 없다.

## 결과

- 비교한 상태와 적용할 때의 상태가 다르면 지문이 달라진다. 이것이 의도한 쓰임이다. 미리 보기는 임대 없이 읽으므로 그 사이에 다른 적용이 끼면 달라질 수 있다.
- 적합성 사례 목록(`Cases`)은 고치지 않고, 따로 `PreviewCases`를 더해 세 저장소(메모리, 파일, PostgreSQL)가 같은 사례를 통과한다.
- 넣지 않은 것: `core.ingest`와의 연결, REST, CLI, 화면, 적용 동작의 변경.
