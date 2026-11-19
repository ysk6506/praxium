---
title: "ADR-0089 새 KG 저장: KG는 자기 스키마에서 스스로 선다"
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOCOV001
  - TLOCK001
  - backend/internal/pgschema/plan_test.go
  - backend/internal/pgstore/kgstore_read_test.go
touches_protected:
  - docs/design-docs/adr/0080-redesign-storage-and-queries-on-one-postgresql.md
  - docs/design-docs/adr/0085-new-kg-storage-implementation-decisions-and-the-schema-plan-unit.md
  - docs/design-docs/adr/0087-schema-executor-and-shared-tables.md
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - harness/config/test-lock.json
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/storage-shape.md
amends:
  - adr: "0085"
    what: 공용에 남는 것의 목록(KG의 목록, 스키마 상태, 번호의 시퀀스와 함수, 저장 형식만)과 KG의 제어 테이블이 저장 스키마에 있다는 것
  - adr: "0080"
    what: 결정 2의 저장 모양에서 머리, 임대, 세대 사건, 확정 버전과 부분합이 공용이 아니라 KG의 스키마에 있다는 것
  - adr: "0087"
    what: 스키마 상태(kg_schema_state)의 자리는 그대로 공용이라는 것의 확인(바뀌는 것은 없다)
amends_tests: []
decision_hash: 556a8255fffd540c
---
# ADR-0089 새 KG 저장: KG는 자기 스키마에서 스스로 선다

## 맥락

ADR-0085가 새 KG 저장의 계획을, ADR-0087이 스키마 실행기와 공용 테이블을 지었다. 이 변경이 그 위에 포트 `graph.Store`의 새 구현(`OpenV2`)을 옛 구현 옆에 더한다. 적용은 제어 행(머리, 임대)과 KG의 테이블을 한 트랜잭션에 쓰므로, 제어 행이 공용에 있으면 KG를 다른 DB로 옮길 때 모든 트랜잭션의 경계가 바뀐다. 새 저장을 지금 짓고 있어 지금 넣는 값이 가장 작다.

## 결정

28. **KG의 제어 행도 KG의 저장 스키마에 둔다.** 머리(변경 번호, 세대, 읽기 워터마크, 적용 중의 상태, 해시 상태, 포기와 복원의 진행 표시, 이력의 하한), 임대, 세대 사건, 확정한 버전과 부분합이 `head`, `lease`, `generation_event`, `version`, `version_hash_part`로 스키마마다 있다. 머리와 임대는 한 줄이고 제자리 갱신이 잦아 채움 비율을 낮춘다. 공용에는 KG의 목록(`kg_catalog`), 스키마 상태, 번호의 시퀀스와 함수, 저장 형식만 남는다. 생성기 버전을 2로 올린다.
29. **`obj_no`와 `link_no`를 받는 일은 함수 하나 뒤에 둔다**(`praxium_next_obj_nos`, `praxium_next_link_nos`). 지금은 공용 시퀀스이고 뒤에 KG마다의 시퀀스나 구간 할당으로 바꿀 수 있다. 번호는 한 번에 필요한 개수만큼 받고, 커밋 순서와 맞지 않아도 되며 다시 쓰지 않는다.
30. **저장의 모든 접근은 "KG → 스키마와 풀"을 푸는 한 곳(`kgAccess`)을 거친다.** SQL에 들어가는 스키마와 테이블의 이름은 `pgschema`의 이름 함수가 정수에서 만든 것뿐이다. 그 한 곳이 어긋남(`pgstore_schema_mismatch`)도 판단한다.
31. **적용의 청크는 대표 키의 순서로 쓴다**(객체는 번호 순, 링크는 `src` 순). 큰 적재 동안의 `max_wal_size`와 체크포인트 간격은 운영 안내에 적는다.
32. **이력의 보존 하한**을 머리에 둔다(처음에는 0). 하한 아래의 과거 읽기는 `graph.CodeNotReadable`로 답한다. 이력과 변경 기록을 변경 번호의 범위로 떼어 지우는 일을 뒤에 넣어도 약속이 깨지지 않는다.
33. **파티션의 전제.** 지금은 파티션하지 않는다. 더해지기만 하는 테이블은 기본키의 앞을 변경 번호로 두고(`object_hist`와 `link_hist`는 `seq_to`로 시작한다), 외래 키로 가리키지 않으며, 파티션의 이름 접두어(`part_`)를 예약한다.

그 밖에 이 변경이 정한 구현의 규칙은 다음과 같다.

- 변동 값의 정확한 값은 `prov`의 속성 번호 항목(`v`, `g`, `b`, `s`, `r`)에 있고, 타입 있는 컬럼은 그 복사본이다(수량 맵과 시각의 오프셋은 타입 있는 컬럼에 손실 없이 들어가지 않는다).
- 임대의 만료는 DB의 시계로 판단한다(기본). 호출자의 시계는 적합성 테스트가 시계를 옮기기 위한 선택이다.
- 변경 번호 `N`을 포기하면 `bound_seq = N`인 `object_key` 줄을 지우고 복원은 지우지 않는다.
- 객체가 처음 만들어질 때의 키는 `graph.ObjectChange.Key`로 저장에 전해진다(`first_key`와 `object_key`의 재료).
- 묶음 읽기는 선택 기능 `graph.BulkReader`이고, 적용기는 있으면 쓴다.
- 커밋의 결과를 모를 때(`pgconn_outcome_unknown`)는 머리를 다시 읽어 판단한다.

## 이유

- 제어 행을 KG의 스키마에 두면 한 KG의 모든 쓰기가 한 스키마 안에서 끝나고, KG를 다른 DB로 옮기거나 지우는 일이 스키마 단위가 된다.
- 번호 발급과 접근을 한 곳에 두면, 뒤에 바뀌는 것(KG마다의 시퀀스, 여러 클러스터)이 그 한 곳에서 끝난다.
- 기본키의 앞이 변경 번호인 더해지기만 하는 테이블은 뒤에 범위로 떼어 내기 쉽다.

## 대안과 기각 사유

- **제어 행을 공용에 둔다.** 이동의 경계가 바뀌고 한 KG의 적용이 공용 테이블의 줄을 두고 다른 KG와 만난다.
- **`ApplyChunk`가 키를 다시 계산한다.** 키는 적용기가 이미 풀어 놓았고, 저장이 모델을 알게 된다.
- **변동 값을 타입 있는 컬럼에만 둔다.** 값이 손실 없이 돌아오지 않는다.

## 결과

- `pgstore`는 `OpenV2`와 `StoreV2`(`graph.Store`의 구현), 테스트용 `NewTestStoreV2`를 내보낸다. 제품의 길은 아직 옛 구현을 쓴다.
- 옛 `pgschema` 테스트는 한 글자도 바꾸지 않고 통과한다. 잠긴 테스트의 선언은 바뀌지 않는다.
- 상한 옵션(ADR-0085 결정 11의 한 트랜잭션 한도)은 이 변경에서 구현하지 않았다.
