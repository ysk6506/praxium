---
title: ADR-0022 표준 모델을 영향 범위에 맞게 보강한다
kind: decision
status: active
enforced_by:
  - GOCOV001
  - GODEBT022
  - GODEBT023
  - backend/internal/model/identity_set_test.go
  - backend/internal/model/standard_test.go
  - backend/internal/model/validate_test.go
  - backend/internal/model/enums_test.go
  - backend/internal/worldgen/groups_test.go
  - backend/internal/worldgen/golden_test.go
  - backend/internal/worldgen/world_prop_test.go
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/kg-model.md
  - docs/design-docs/standard-model.md
  - docs/design-docs/worldgen.md
  - docs/exec-plans/active/plan.md
  - docs/exec-plans/tech-debt.yaml
touches_protected: []
amends_tests:
  - backend/internal/model/standard_test.go::TestStandardCounts
  - backend/internal/model/standard_test.go::TestStandardIsDeterministic
  - backend/internal/model/standard_test.go::TestStandardIdentityKeysResolve
  - backend/internal/model/standard_test.go::TestStandardKeylessTypesAreLocked
  - backend/internal/model/enums_test.go::TestCodeCatalogueIsStable
  - backend/internal/worldgen/golden_test.go::TestGoldenTinyWorld
  - backend/internal/worldgen/world_prop_test.go::TestEveryLayerAndStructure
test_change_reason: 표준 모델의 데이터(키, 바인딩, 식별자 방식)와 생성기의 이중화 구조가 바뀌어 잠근 숫자와 해시와 목록이 달라진다. 값은 아래 표의 옛 값에서 새 값으로 의도해서 바꾼다. Go 테스트는 아직 테스트 잠금이 읽지 않아(TD-0008) 이 결정이 하나씩 적는다.
decision_hash: 00f07f6b56cdd26d
---
# ADR-0022 표준 모델을 영향 범위에 맞게 보강한다

## 맥락
ADR-0020이 표준 모델을 데이터 파일로 굳혔고 ADR-0021이 그 위에 합성 세계 생성기를 세웠다. 둘을 쓰다 보니 영향 범위를 제대로 따라가기에 모자란 곳이 드러났다. (1) 타입 20개에 식별 키가 없었다(TD-0009). (2) 이중화 그룹이 구조가 아니었다: 서버와 랙이 구성원 하나(항온항습기, 스위치)에 직접 이어졌고, 시설의 묶음(냉각 N+k, UPS)과 서비스의 구성원 묶음은 객체로 쓸 바인딩이 없었다(`worldgen.md` 13절 물음 1~3). (3) 응용 서비스 사이의 `DEPENDS_ON`이 없었다. 설계 담당이 방향을 정했다: 이 결정은 그 방향을 옮긴다. 링크 뜻 열하나와 채널 여섯(ADR-0007)은 그대로이고 새 링크 타입과 타입은 없다.

## 결정
- **키의 구성요소가 늘어난다.** 한 키에 문맥 구성요소를 여럿 둘 수 있다. 새 종류 **집합**: 링크 타입 하나로 닿는 객체들의 내부 ID를 정렬한 것이 키의 한 조각이다(`set`). 나가는 링크를 읽거나 `incoming`으로 들어오는 링크를 읽고, `size`로 정확한 수를 요구할 수 있다(0은 아무 수). 집합과 문맥만 있는 키(속성과 방식이 없는 키)는 서 있을 수 있다: 집합은 그 자체로, 문맥은 링크가 **1:1**일 때만. 새 코드 `model_identity_set`(링크 타입이 없음, 그 끝에 바인딩이 없음, 집합이 아닌 것에 `incoming`이나 `size`, 음수 크기).
- **해시.** `set`, `incoming`, `size`는 값이 있을 때만 해시 트리에 들어가서 집합 없는 키의 해시 규칙(버전 1)은 그대로이다. 번호 해시는 그대로이다.
- **모든 표준 타입이 자기 키를 가진다.** 키 없는 목록은 비고 테스트가 잠근다(`TestStandardEveryTypeHasAKey`). 20개의 키: `CoolingLoop` (LOCATED_IN 방, 이름), `Circuit` (OWNED_BY, 회선 id), `Contract` (OWNED_BY 집합, 계약 번호), `EcmpGroup` (PART_OF, 이름), `Endpoint` (주소나 호스트 이름, 포트, 프로토콜; 약함), `HostAggregate`와 `PlacementRule` (PART_OF 가상화 관리자, 이름), `InitiatorGroup` (PART_OF 어레이, 이름), `SanZone` (PART_OF 패브릭, 이름), `IpAddress` (PART_OF 프리픽스, 주소), `LogicalGroup` (이름; 약함), `MlagDomain` (MEMBER_OF 들어오는 집합, 크기 2), `OnCallSchedule` (OWNED_BY 집합, 일정 이름), `OverlaySegment` (REALIZED_BY 집합, VNI), `PowerCircuit`과 `PowerFeed` (LOCATED_IN 건물, 회로 id나 이름), `RackUnit` (PART_OF 랙, U 위치, 면), `RoutingSession` (PART_OF 라우팅 프로세스, 로컬 주소, 원격 주소), `Tunnel` (TUNNELS_VIA 집합, 이름), `UnknownObject` (힌트 해시 방식, 약함). 문맥만 있는 키 둘: `Transceiver`(PART_OF 인터페이스를 1:1로 고침)와 `ControlPlane`(PART_OF 컨테이너 클러스터, 1:1).
- **식별자 방식** `is.hint_hash`(정규화 값 타입 `vt.string`)를 더한다: 값은 받은 단서(기대한 타입, 단서 글, 가리킨 곳)를 다듬어 섞은 해시이다.
- **이중화 그룹은 구조이다.** 구성원은 `MEMBER_OF`로 묶음에 들고 쓰는 쪽은 묶음에 `supplied_by`(전원, 냉방, 네트워크), `requires`(서비스, 클러스터 용량, 저장)나 `realized_by`로 기댄다. 구성원 하나에 직접 잇지 않는다. 영역: 냉방(구역마다 `CoolingRedundancyGroup`, 사이트의 냉동기 묶음, 서버의 팬 묶음), 전원(면마다 UPS 묶음, 서버의 피드 장치 2N), 네트워크(랙마다 MLAG 짝, ToR의 LAG, 집선의 ECMP, 가장자리 HA 짝), 저장(컨트롤러 HA 짝에 볼륨이 `RUNS_ON`), 클러스터 용량(`ComputeCluster`에 기술 서비스가 `REALIZED_BY`), 서비스(`LbPool`, `DatabaseCluster`, `Workload`, `ControlPlane`). 응용 서비스 사이는 `DEPENDS_ON`(이어서 쓰는 의존)이고 `CALLS`는 호출이다. 랙에 꽂는 장비의 자리는 `MOUNTED_IN`(`contained_in`)이 하나로 말하고 같은 사실의 `LOCATED_IN`은 없다.
- **더한 바인딩 23개와 고친 것 하나.** 키의 부모: `bd.located_in.cooling_loop.room`, `bd.located_in.power_circuit.building`, `bd.located_in.power_feed.building`, `bd.part_of.host_aggregate.virtualization_manager`, `bd.part_of.placement_rule.virtualization_manager`, `bd.part_of.initiator_group.storage_array`, `bd.part_of.san_zone.fabric`, `bd.part_of.rack_unit.rack`. 묶음의 부모: `bd.part_of.redundancy_group.room`, `...building`, `...application_service`. 구성원: `bd.member_of.crah.cooling_redundancy_group`, `bd.member_of.chiller.cooling_redundancy_group`, `bd.member_of.ups.power_redundancy_group`. 쓰는 쪽: `bd.cooled_by.cooling_loop.cooling_redundancy_group`, `bd.uplinked_via.physical_server.mlag_domain`, `bd.uplinked_via.network_device.mlag_domain`, `bd.uplinked_via.hypervisor_host.mlag_domain`, `bd.uplinked_via.network_device.ha_pair`, `bd.runs_on.workload.control_plane`, `bd.realized_by.technical_service.workload`, `bd.depends_on.application_service.application_service`, `bd.depends_on.application_service.redundancy_group`. 고친 것: `bd.part_of.transceiver.interface`가 N:1에서 1:1이다. 번호(`type_no`, `link_type_no`, `attr_no`)와 옛 바인딩 id는 그대로이고 테스트가 잠근다(`TestStandardNumbersAreStable`). 타입, 링크 타입, 특성, 속성은 늘지 않는다.
- **생성기.** 서버, 랙, 구역, 사이트가 위 묶음을 객체로 만들고 쓰는 쪽이 묶음에 기댄다. 검증기가 세 가지를 지킨다: 구성원 수가 `min_alive` 이상, 묶음에 기대는 객체가 하나 이상, `POWERED_BY`, `COOLED_BY`, `UPLINKED_VIA`가 구성원 하나로 가지 않음. 키를 만들 수 없는 객체와 키 없는 타입을 표시하는 길은 확장 타입을 위해 남긴다. 분포 프로파일과 매니페스트의 형식은 바뀌지 않아 버전은 그대로이다(`worldgen.md`).

## 테스트의 옛 값과 새 값
| 테스트 | 옛 값 | 새 값 |
|---|---|---|
| `TestStandardCounts` 바인딩 | 528 | 551 |
| `TestStandardIsDeterministic` 모델 해시 | `b8a998d4b4c7ac9013ec7e4a89ce47cbfcfbdf87a57fac96370ecfb4ddd47fe8` | `712b5ae602cd347af9b4eecaa975b3637b5e96ccaa9d8682064d31749a07f98a` (번호 해시 `3b9b3505…1f1e`는 그대로) |
| `TestStandardIdentityKeysResolve` 식별자 방식 | 36 | 37 |
| 같은 테스트의 키, 문맥 구성요소 | 215, 99 | 235, 116 (그 가운데 집합 5, 옛 값 0) |
| `TestStandardKeylessTypesAreLocked` | 키 없는 타입 20개의 목록(`CoolingLoop`, `Circuit`, `Contract`, `EcmpGroup`, `Endpoint`, `HostAggregate`, `InitiatorGroup`, `IpAddress`, `LogicalGroup`, `MlagDomain`, `OnCallSchedule`, `OverlaySegment`, `PlacementRule`, `PowerCircuit`, `PowerFeed`, `RackUnit`, `RoutingSession`, `SanZone`, `Tunnel`, `UnknownObject`) | 빈 목록, 테스트 이름은 `TestStandardEveryTypeHasAKey` |
| `TestCodeCatalogueIsStable` | 코드 38개 | 코드 39개(`model_identity_set` 더함) |
| `TestGoldenTinyWorld` 객체와 링크 | 1385, 2437 | 1391, 2505 |
| 같은 테스트의 내용 해시 | `1ea25c49425d67c13b323e51fcbd3a10a7bf1a1399b3a5b8ebea284dd4ac3626` | `8d648b7bfd6e370de5163d17a9c1a9a8d196cac2fe8c19a2df518621c60b4085` |
| 같은 테스트의 매니페스트 해시 | `93f73e124853204c8689c09a352d6b6526ab6b05428ed4fdd028069bc734bdef` | `cd5f56fb86006bf1bd59b5ec13c44f1806b42347000d1ef79e5e20623994017a` |
| `TestEveryLayerAndStructure` | 수집 불가 객체와 링크가 있어야 한다(`CoolingLoop`) | 없어야 한다(0, 0), 타입 목록에 `CoolingRedundancyGroup`과 `MlagDomain` 추가 |

테스트 이름 단위의 나머지(타입 수 168, 링크 타입 55, 특성 19, 속성 671, 액션 112, 값 타입 4, 번호 해시, 분포 프로파일과 규모의 테스트)는 바뀌지 않았다. 더한 테스트: 집합과 문맥만 있는 키의 실패 예 아홉과 받아들이는 예, 해시 구분, 번호 안정, 묶음의 구성원과 쓰는 쪽이 묶일 수 있음, `MOUNTED_IN`의 뜻, 영역마다 묶음이 있음, 검증기의 묶음 규칙의 실패 예(`groups_test.go`).

## 이유
- 영향 범위는 "누가 무엇에 기대는가"를 따라간다. 묶음이 객체여야 `a >= k`를 셀 수 있고, 쓰는 쪽이 묶음에 기대야 한 구성원의 죽음이 여유를 줄이는 것으로 계산된다.
- 키 없는 타입이 있으면 어떤 원천도 그 객체를 채울 수 없다. 집합 구성요소는 끝점 쌍이나 구성원 목록이 이름인 타입(MLAG 짝, 터널)을 순서에 상관없이 알아보게 한다.
- 정렬한 내부 ID를 쓰고 해시에는 값이 있을 때만 새 칸을 넣어서 옛 데이터와 해시 규칙이 이어진다.

## 대안과 버린 이유
- **링크 뜻이나 채널 더하기**: 고정 목록(ADR-0007)을 건드리지 않고도 된다.
- **구성원에 직접 잇고 묶음은 수로만 두기**: 여유와 `slack`을 계산할 수 없다.
- **키 없는 타입을 확장 타입이 채우게 하기**: 표준 타입의 수집이 풀리지 않는다.

## 결과
- TD-0009를 닫는다. 이중화 그룹 객체가 늘어 세계가 조금 커진다(같은 인자에서 객체 6개, 링크 68개 더).
- 남은 일: 분포 프로파일에 영역별 묶음 수를 더할지(형식 버전은 그대로 두었다), 구성원이 하나인 UPS 묶음을 묶음으로 둘지(`worldgen.md` 물음 4).
