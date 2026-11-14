---
title: ADR-0020 타입 등록부와 표준 모델의 데이터 파일을 도입한다
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH011
  - GOARCH021
  - GOARCH051
  - GOARCH052
  - GOARCH053
  - GOTEST002
  - GOCOV001
  - backend/internal/model/validate_test.go
  - backend/internal/model/registry_test.go
  - backend/internal/model/standard_test.go
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/kg-model.md
  - docs/design-docs/standard-model.md
  - README.md
  - docs/exec-plans/active/plan.md
touches_protected:
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
amends_tests: []
decision_hash: c9455b9fdd14ba98
---
# ADR-0020 타입 등록부와 표준 모델의 데이터 파일을 도입한다

## 맥락
ADR-0007이 모델의 필드와 규칙을 정했고 표준 모델은 "데이터 파일로 적재할 때 확정된다"고 적었다. 이 변경이 Go 단위 `core.model`(`backend/internal/model`)과 그 데이터 파일을 처음 도입한다.

## 결정
- **단위.** `core.model`은 `core.kernel`만 읽는다. 도메인 타입, 검증, 불변 등록부, 표준 모델의 데이터 파일을 가진다.
- **데이터는 JSON.** 표준 라이브러리에 YAML 파서가 없고 새 의존을 추가하지 않는다. 파일은 `backend/internal/model/standard/`에 있고 `go:embed`로 포함한다. 이 파일이 표준 모델의 단일 진실 공급원이고 `standard-model.md`는 요약이다. 읽을 때는 알 수 없는 필드를 오류로 처리한다.
- **번호.** 파일에 번호를 적을 수 있고(`type_no`, `link_type_no`, `attr_no`), 적지 않은 것은 ID 오름차순으로, 사용된 최댓값(예약 포함) 다음 번호부터 부여한다. 한 번 부여한 번호는 변경하지 않고, 이름을 변경해도 ID와 번호는 같다. 번호는 `reserved` 목록으로 예약해 둘 수 있다.
- **해시.** 모델 해시는 ID, 의미, 관계, 위험 필드 같은 정의의 내용으로 커널의 정규 인코딩과 SHA-256을 사용해 산출한다. 이름, 별칭, 설명, 표시 이름, 연락처, 번호는 포함하지 않는다(REQ-233). 번호는 별도의 `NumberingHash`가 다룬다. 해시 규칙의 버전은 1이다.
- **오류 코드.** 커널의 코드 목록은 테스트가 잠그고 있어 이 단위는 `model_` 접두사의 코드와 목록(`Codes`, `CodeMeaning`)을 자기 테스트로 잠근다.
- **표준 모델의 수.** 조사 담당의 전체 초안을 데이터 파일로 옮겼고, 나중에 정해진 결정과 충돌하는 곳은 그 결정이 우선한다.

| 항목 | 초안 | 데이터 파일 |
|---|---|---|
| 객체 타입 | 168(핵심 122, 확장 46) | 168(핵심 122, 확장 46) |
| 링크 타입 이름 | 58 | 55 |
| 바인딩 | 표의 행 157 | 528(출발과 도착의 쌍마다 하나) |
| 액션 타입 | 112 | 112 |
| 특성 | 16 | 19(공통 속성의 `Common`, 부품 조립체 `HardwareAssembly`, 망의 바깥 `NetworkAnchor`) |

링크 이름이 3개 줄었다. `INSTALLED_IN`과 `REQUIRES_PART`는 `PART_OF`로 통합했다(의미 `part_of`, 부품 측 전파는 바인딩의 `whole_needs_part`. 방향은 부품에서 전체). `SUPPLIED_BY`(리소스 `fuel`)는 고정된 채널 6종에 연료가 없어 제외했고, 그래서 `Generator`와 `FuelTank`를 연결하는 바인딩이 없다. 바인딩은 표의 행 하나가 여러 쌍을 뜻해 쌍으로 세면 늘어난다.

- **바인딩 펼치기.** 표의 행 하나가 출발 여러 타입이나 도착 여러 타입을 뜻하면 출발과 도착의 쌍마다 바인딩 하나로 펼친다. 표의 행 157개가 바인딩 528개가 된다.
- **키로 표현하지 못한 것.** 데이터에 키 213개를 담았고 초안의 키 35개가 빠졌으며 이 때문에 키가 하나도 없는 타입은 20개다(OnCallSchedule, Contract, PlacementRule, HostAggregate, InitiatorGroup, SanZone, MlagDomain, IpAddress, RoutingSession, EcmpGroup, Tunnel, OverlaySegment, Circuit, RackUnit, PowerCircuit, PowerFeed, CoolingLoop, Endpoint, LogicalGroup, UnknownObject). 한 부류는 초안의 링크 행에 부모로 가는 N:1 바인딩이 없어서 문맥을 구성할 수 없는 키이고(Rack 안의 RackUnit, Fabric 안의 SanZone 따위. 바인딩을 지어내지 않았다), 다른 부류는 모델이 표현하지 못하는 키다. 문맥만 있는 키(Transceiver), 집합 구성(Cable의 양 끝 집합, MlagDomain의 멤버 집합), 문맥 2개(EcmpGroup, ApplicationService), 끝점 쌍(Tunnel), 값이 아닌 객체 참조(Circuit의 사업자), 힌트 해시(UnknownObject)이다.

- **나중 결정이 우선한 곳.** 링크 의미는 고정 11가지, 채널은 고정 6가지다. 링크는 `volatile`일 수 없다. 10번째 층 `system`에는 표준 타입이 없다. 속성 변경 속도(`change_class`)는 초안이 적지 않아 이름으로 정했다(`_state`로 끝나는 것 따위는 `dynamic`, 제조 식별은 `static`, 나머지는 `slow`). 링크 의미의 속성 값과 겹치는 파라미터(`weight`, `role`, `strength`)는 링크 속성으로 두지 않았다. 사람의 속성(`role_title`)은 두지 않았다. 담당자는 연락처로 둔다(ADR-0012).
- **식별 키.** 키는 순서 있는 구성요소 목록, 강/약, 식별자 방식(선택)이다. 구성요소는 타입의 속성이거나 문맥(링크 타입이고, 그 링크로 도달하는 객체 안에서 유일하다)이다. 모든 키는 우선순위 순서로 두고 해시에 포함된다. 식별자 방식은 별도 등록 항목(이름, 네임스페이스, 설명, 정규화 값 타입, 36개, `identifier_schemes.json`)이고 외부 방식(`ext:...`)도 여기에 선언한다. 검증 규칙과 코드: 구성요소가 있을 것, 변하는(`volatile`) 속성이 키에 없을 것(`model_identity_key_volatile`), 문맥 링크에 그 타입에서 나가는 N:1 또는 1:1 바인딩이 있을 것(`model_identity_context`), 방식이 있을 것(`model_scheme_unknown`), 키가 겹치지 않을 것. 각각 실패 예가 테스트에 있다. 제조사와 `serial_number`의 조합은 강한 키, `serial_number` 하나는 약한 키다. `serial_number`는 키를 대조할 때만 앞뒤 공백을 제거하고 대문자로 취급하며 저장은 수신한 그대로다. 키에 필요해 속성이 추가된 곳이 67곳이다.
- **채택한 해석.** 초안의 `required`는 `*`로 적은 것, 초안 0.7절의 목록, 첫 식별 키의 속성, `Common`의 `name`과 `lifecycle_state`만이다. 특성이 제공하는 속성은 특성 전체에 적용돼 타입마다 다르게 필수로 지정할 수 없다. 열거 값이 없는 속성은 문자열이다. 범위 키(`scope_key`)는 설계 문서가 이름을 지정하지 않아 없다. 하위 타입이 상위와 같은 이름의 속성을 다른 값 목록으로 다시 적은 곳(예: `EcmpGroup`의 `derived_from`)은 상위 것을 사용한다. 초안의 표를 달리 읽을 수 있는 `DEPENDS_ON`의 "서비스"는 BusinessService, TechnicalService, ApplicationService로 해석했다. `REPLACES`의 "동일 타입"은 초안이 열거한 6개 타입 각각의 자기 자신으로, `MEMBER_OF`의 `~member`와 이를 가리키는 행은 각 층의 행이 정하므로 없다. `Firmware`는 `HasFirmware`를 가진 것에 `PART_OF`다.
- **층의 이름.** `facility`, `hardware`, `network`, `storage`, `virtualization`, `container`, `platform`, `service`, `cross_cutting`, `system`.

## 남은 일
키가 없는 타입 20개는 TD-0009로 남긴다. 수집 단계가 그 타입의 객체를 생성하기 전에 해결해야 하고, 설계 담당이 정한 방향은 다음과 같다. (1) 키는 문맥 구성요소를 2개 이상 가질 수 있다. (2) 이름 붙은 링크로 도달하는 순서 없는 객체 집합을 새 구성요소 종류로 두고 멤버 ID를 정렬해 정규화한다(끝점 쌍, 멤버 집합). (3) 문맥 링크가 1:1이면 문맥만 있는 키를 허용한다. (4) 키가 요구하는 부모 바인딩은 그 후속 변경에서 표준 모델에 일반 바인딩으로 추가한다. 여기서 지어내지 않는다. (5) `UnknownObject`에는 힌트 해시 방식을 선언한다. 테스트가 키 없는 타입 목록을 잠가 수가 의도한 경우에만 줄어들게 한다.

## 결과
- 수와 이름이 문서와 데이터 사이에서 불일치하면 데이터가 맞고, 문서를 수정한다.
- 표준 모델의 해시가 바뀌는 변경은 테스트(`standard_test.go`)가 실패해 결정 기록을 요구한다.

## 기각
- YAML 데이터: 새 의존이 필요하다.
- 커널 코드 목록에 코드 추가: 커널의 잠금 테스트를 수정해야 한다.
