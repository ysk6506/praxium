---
title: ADR-0065 바인딩 우선순위와 인자가 정해지는 순서를 구현한다 (ADR-0059의 0단계 앞부분)
kind: decision
status: active
enforced_by:
  - backend/internal/model/binding_answers_test.go
  - backend/internal/model/binding_priority_test.go
  - backend/internal/ingest/cardinality_priority_test.go
updates_docs:
  - docs/design-docs/model-links.md
  - docs/design-docs/kg-model.md
  - docs/design-docs/standard-model.md
  - docs/product-specs/requirements-model-storage.md
touches_protected:
  - docs/design-docs/adr/0059-model-links-and-redundancy-loss.md
  - docs/product-specs/requirements-model-storage.md
amends:
  - adr: "0059"
    what: 0단계에서 세 바인딩을 N:M으로 넓힌다는 부분. 둘만 넓히고 Circuit→Organization은 N:1로 둔다.
amends_tests:
  - go:backend/internal/model::TestStandardIsDeterministic
  - go:backend/internal/model::TestStandardHashIsTheSameWithNoOverlay
  - go:backend/internal/model::TestOverlayOnTheStandardModel
  - go:backend/internal/worldgen::TestGoldenTinyWorld
  - go:backend/internal/model::TestBindingForGivesTheFirstBindingOfOneLinkType
  - go:backend/internal/model::TestBindingsForAPair
test_change_reason: 우선순위가 id 순에서 양쪽 끝이 좁은 순으로 바뀌어(REQ-421) 이기는 바인딩과 목록의 순서가 달라지고, links.json이 바뀌어 모델 해시와 골든 매니페스트 해시가 달라진다. 단언의 종류와 개수는 그대로이고 기댓값만 아래 표의 새 값으로 바꾼다.
decision_hash: 8611bc1910baff4b
---
# ADR-0065 바인딩 우선순위와 인자가 정해지는 순서를 구현한다

## 맥락

ADR-0059가 표준 모델의 바인딩 보강을 단계로 나눴고, 0단계는 영향 범위의 답을 바꾸지 않으면서 우선순위, 바인딩의 기본값, 인자가 정해지는 순서를 넣는 것이다. 이 결정은 0단계의 앞부분을 구현한다. 링크에 쓴 값(REQ-420)은 넣지 않는다.

## 결정

1. **우선순위 (REQ-421).** 맞는 바인딩이 여럿이면 양쪽 끝이 좁은 것이 이긴다. 끝의 점수는 any 0, 트레이트 1, 타입 2 + 깊이(조상 타입의 수)이고 바인딩의 점수는 두 끝의 합이다. 같으면 바인딩 id 순이다. 판단은 `model` 패키지의 비교 함수 하나(`better`)에 있고, `BindingFor`, `BindingsFor`(같은 링크 타입의 첫 항목이 `BindingFor`의 답), 수집의 카디널리티 검사, 영향 범위의 인자 읽기가 그 결과를 쓴다. 점수는 레지스트리를 만들 때 한 번 계산한다. 조직이 더한 정의에도 같은 규칙이 걸린다.
2. **`param_defaults`.** `Binding`에 인자 이름 → 값의 기본값을 더한다. 검증 규칙은 둘이다. (가) 기본값은 enum 인자여야 하고 그 바인딩이 허용하는 값(링크 타입이 고정한 값, 없으면 바인딩의 제약, 없으면 의미의 모든 값) 안에 있어야 한다. (나) 허용 값이 여럿인데 의미의 기본값(`strength`는 hard, `whole_needs_part`는 none)이 그 안에 없으면 `param_defaults`가 반드시 있어야 한다. 모델 해시에는 비어 있지 않을 때만 들어간다.
3. **값이 정해지는 순서.** `model.ResolveParams` 한 함수가 정한다. 링크 타입이 고정한 값, 링크에 쓴 값, 바인딩의 기본값, 허용 값이 하나뿐이면 그 값, 의미의 기본값의 순서다. 2번(링크에 쓴 값)은 인자(`written`)로 자리만 있고 지금은 모든 호출자가 nil을 넘긴다. 다음 결정이 채운다. "허용 값이 여럿이면 soft로 읽는다"는 없앴고, 그렇게 읽히던 `PART_OF` `VirtualDisk`→`VirtualMachine`에 `param_defaults`로 `whole_needs_part`의 soft를 적어 답이 그대로다.
4. **확인한 수.** 맞는 바인딩이 둘 이상인 (링크 타입, 시작 타입, 끝 타입) 조합이 226개이고 이 가운데 20개에서 이기는 바인딩이 바뀐다(설계의 추정과 같다). POWERED_BY 11, OWNED_BY 4, REALIZED_BY 2, MANAGED_BY 1, UPLINKED_VIA 1, IN_FAILURE_DOMAIN 1이고 목록은 `testdata/binding_winners.txt`에 있다. 20개 모두 인자가 같고 점수가 같은 조합은 없다.
5. **N:M으로 넓히는 둘과 N:1로 두는 하나.** 카디널리티가 N:M에서 N:1이 되는 조합은 `VirtualMachine`→`VirtualizationManager`, `ClusterNode`→`FailureDomain`, `Circuit`→`Organization` 셋이다. 앞의 둘은 새로 이기는 좁은 바인딩을 N:M으로 넓혀 기존 자료를 새로 거부하지 않는다. `Circuit`→`Organization`(`bd.owned_by.circuit.organization`)은 N:1로 둔다. `Circuit`의 식별 키가 OWNED_BY를 문맥으로 쓰고 모델 검증(`checkKeyContext`)이 `Circuit`에서 시작하는 N:1 바인딩을 요구해서 넓히면 표준 모델이 열리지 않기 때문이고, 소유 조직은 하나여야 맞다. 예전에 N:M으로 통과하던 것은 넓은 바인딩이 id 순으로 이겨서 생긴 불일치였다. ADR-0059가 "세 바인딩을 넓힌다"고 적은 부분을 이렇게 고친다. 그 결과 소유 조직이 둘 이상인 `Circuit` 자료는 수집에서 id 순으로 앞의 링크만 남고 둘째 링크가 문제 목록(`cardinality`)으로 빠진다.

## 이유

- 같은 판단이 두 곳에 있으면 읽는 곳마다 다른 바인딩을 뽑는다. 한 함수에 두면 같은 입력이 언제나 같은 결과를 낸다.
- 의미의 기본값만으로는 허용 값이 여럿인 바인딩의 값을 정할 수 없다. 모델이 말해야 하고, 모르는 채로 약한 쪽을 고르는 규칙은 숨은 가정이다.
- 영향 범위의 답을 바꾸지 않는 변경이므로 먼저 지금의 답을 떠 두고(`testdata/binding_answers.txt`) 같은지 확인하는 것이 완료 기준이다.

## 완료 기준의 테스트

- `binding_answers_test.go`: 모든 (링크 타입, 시작 타입, 끝 타입)의 카디널리티와 인자가 코드를 고치기 전에 뜬 표와 같다. 표는 한 줄만 다르다. `Circuit`→`Organization`이 N:1이다(5항).
- `binding_priority_test.go`: 226개와 20개의 목록, `BindingsFor`와 `BindingFor`의 일치, 설계의 예 셋, 점수가 같을 때의 id 순, 조직이 더한 바인딩, `param_defaults` 검증의 실패 사례, `ResolveParams`의 순서.
- `cardinality_priority_test.go`: 소유 조직이 둘인 `Circuit`은 둘째 링크가 문제 목록으로 빠지고, 넓힌 둘은 여럿을 받는다.

## 대안과 기각 사유

- `Circuit`→`Organization`을 N:M으로 넓히고 식별 문맥 검증을 느슨하게 한다: 검증 규칙을 바꾸는 일이고 식별 키의 뜻(소유자 하나)과 어긋나서 기각한다.
- 순서의 2번까지 한 번에 넣는다: 링크의 필드, 수집 매핑, 저장, 근거 등급이 정해지지 않아 기각한다.

## 결과

- REST의 바인딩 조회(`param_defaults`, 비어 있으면 생략)와 CLI 출력에 기본값이 나온다.
- 잠긴 테스트 여섯의 기댓값이 아래 표처럼 바뀐다. 해시가 아닌 기댓값은 둘이고 둘 다 순서 단언이다.

| 테스트 | 옛 값 | 새 값 |
|---|---|---|
| `TestStandardIsDeterministic`, `TestStandardHashIsTheSameWithNoOverlay`, `TestOverlayOnTheStandardModel`의 표준 모델 해시 | `712b5ae6…07f98a` | `2d42f2f3…74a6bf` |
| `TestGoldenTinyWorld`의 매니페스트 해시 | `5bbc705e…a33f6e` | `e2af04bd…eea0` |
| `TestBindingForGivesTheFirstBindingOfOneLinkType`의 `server`→`rack` MOUNTED_IN | `bd.mounted_in.mountable.rack`(id 순) | `bd.mounted_in.server.rack`(타입이 트레이트보다 좁다) |
| `TestBindingsForAPair`의 `server`→`rack` 목록 | mountable.rack, server.rack, powered.any | server.rack, mountable.rack, powered.any |
