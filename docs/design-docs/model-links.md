---
title: 표준 모델의 바인딩과 이중화 상실의 설계
kind: guide
status: active
follows:
  - docs/design-docs/adr/0059-model-links-and-redundancy-loss.md
---
# 표준 모델의 바인딩과 이중화 상실의 설계

[ADR-0059](adr/0059-model-links-and-redundancy-loss.md)의 자세한 내용이에요. **모두 아직 구현하지 않은 설계(예정)예요.** 지금 코드는 이 문서의 "지금 코드" 열이나 "코드를 읽은 판단"으로 적은 대로 움직여요. 숫자와 판정은 조사에서 나온 것이고 공개 문서로 하나하나 확인하지 않았어요(7절).

## 1. 바인딩 우선순위의 정의

링크 타입 하나에 맞는 바인딩이 여럿이면 지금은 바인딩 id 문자열이 가장 작은 하나가 이겨요. `cardinality`, `failover_to`, 인자가 모두 그 바인딩에서 나와요. 새 정의는 양쪽 끝이 더 좁은 바인딩이 이기게 해요.

- 끝의 점수: any는 0, 트레이트는 1, 타입은 2 + 깊이예요. 깊이는 조상 타입의 수이고 뿌리 타입은 0이에요. 타입의 하위 타입도 맞는 것으로 봐요.
- 바인딩의 점수: 시작 끝과 도착 끝의 점수의 합이에요. 큰 쪽이 이겨요.
- 점수가 같으면 지금처럼 바인딩 id 순이에요. 지금 표준 모델에는 같은 점수가 없어요(`links.json`을 읽고 계산한 추정).
- 알려진 조합이 먼저예요. 알려진 조합이 하나도 맞지 않을 때만 일반 규칙(3절)을 써요.

### 예 셋

| 링크 | 맞는 바인딩과 점수 | 이기는 것 |
|---|---|---|
| `Chiller` POWERED_BY `PowerRedundancyGroup` (`Chiller` 깊이 0, `PowerRedundancyGroup` 깊이 1) | 트레이트(전원을 받는 것)→전원 원천 트레이트 2, `Chiller`→전원 원천 트레이트 3, 트레이트(전원을 받는 것)→`PowerRedundancyGroup` 4 | 마지막(4). 지금은 id 순이라 `chiller`→전원 원천 트레이트가 이겨요. |
| `StorageArray` UPLINKED_VIA `SanSwitch` (`StorageArray` 0, `SanSwitch` 1) | `storage_array`→`network_device` 4, `storage_array`→`san_switch` 5 | `san_switch`(5). 지금은 `network_device`가 이겨요. |
| `VirtualMachine` MANAGED_BY `VirtualizationManager` (둘 다 깊이 0) | 트레이트(관리 대상)→`virtualization_manager` 3, `virtual_machine`→`virtualization_manager` 4 | `virtual_machine`(4). 카디널리티가 N:M에서 N:1이 되므로 수집의 검사가 엄격해지는 예예요. |

### 효과의 크기 (추정)

`links.json`을 읽기만 해서 계산한 추정이에요.

- 맞는 바인딩이 둘 이상인 (링크 타입, 시작 타입, 끝 타입) 조합이 226개예요. 이 가운데 20개에서 이기는 바인딩이 바뀌어요.
- POWERED_BY 11개: `Transformer`→`UtilityFeed`, `TransferSwitch`→`PowerRedundancyGroup`, `Ups`→`Switchgear`, `Ups`→`TransferSwitch`, `Ups`→`PowerRedundancyGroup`, `Chiller`·`CoolingTower`·`Crah`·`Crac`·`InRowCooler`·`Cdu`→`PowerRedundancyGroup`. OWNED_BY 4개: `Tenant`→`Team`, `OnCallSchedule`→`Team`, `Contract`→`Organization`, `Circuit`→`Organization`. REALIZED_BY 2개: `TechnicalService`→`Workload`, `ApplicationService`→`Workload`. 그리고 MANAGED_BY 1개(`VirtualMachine`→`VirtualizationManager`), UPLINKED_VIA 1개(`StorageArray`→`SanSwitch`), IN_FAILURE_DOMAIN 1개(`ClusterNode`→`FailureDomain`)예요.
- 20개 모두 `param_constraints`가 같아서 영향 범위의 답은 바뀌지 않아요.
- 카디널리티가 N:M에서 N:1이 되는 것은 `VirtualMachine`→`VirtualizationManager`, `Circuit`→`Organization`, `ClusterNode`→`FailureDomain` 셋이에요. 새로 이기는 좁은 바인딩을 N:M으로 넓혀서 기존 데이터를 새로 거부하지 않아요. 좁히는 시점은 미결 사항이에요.

## 2. 링크별 인자와 값이 정해지는 순서 (0단계)

링크 하나가 `cardinality`, `whole_needs_part`, `strength`, `failover_to` 같은 인자의 값을 직접 가질 수 있어요. 값은 이 순서로 정해져요.

1. 링크에 쓴 값
2. 바인딩의 기본값(새 항목)
3. 허용된 값이 하나뿐이면 그 값
4. 링크 의미의 기본값

지금 코드는 허용된 값이 여럿이면 `whole_needs_part`를 soft로, `strength`를 hard로 읽어요(코드를 읽은 판단). 0단계는 값이 여럿 허용된 바인딩마다 지금 읽는 값을 바인딩의 기본값으로 적어서 영향 범위의 답이 바뀌지 않게 해요. 예: `PART_OF` `VirtualDisk`→`VirtualMachine`은 `{hard, soft}`가 허용되고 기본값은 soft예요. 수집의 검사는 바뀔 수 있어요(우선순위가 이기는 바인딩을 바꾸기 때문이에요).

## 3. 바인딩: 알려진 조합과 일반 규칙

- 지금의 바인딩은 알려진 조합이에요. 조사에서 거부된 흔한 조합은 알려진 조합으로 올려요(6절).
- 일반 규칙은 알려진 조합이 하나도 맞지 않을 때만 써요. 이 규칙으로만 통과한 링크는 거부하지 않고 문제 목록에 알림을 남겨요.

| 규칙 | 내용 | 비고 |
|---|---|---|
| 1 | 누구나 그룹의 구성원이 될 수 있어요. 시작 끝은 any예요. | "장애가 날 수 있는" 것으로 좁히는 트레이트는 미결 사항이에요. 모든 타입에 `tr.common`이 붙어 있어서 그 트레이트로는 좁혀지지 않아요. |
| 2 | 그룹이 그룹의 구성원이 될 수 있어요. | 순환은 금지해요. 검사를 어디에 두는지는 미결 사항이에요. |
| 3 | 구성원에게 의존할 수 있는 곳에서는 그룹에도 의존할 수 있어요. | `runs_on`, `requires`, `supplied_by`, `realized_by`, `managed_by`에 적용해요. |

모든 그룹 하위 타입(`Lag`, `RaidGroup`, `EcmpGroup`, `HaPair`, `QuorumGroup`, `VirtualChassis`, `ReplicationGroup`, `PowerRedundancyGroup`, `ComputeCluster` 등)은 `RedundancyGroup` 아래에 있어요. `LogicalGroup`은 그 아래에 없어요.

경로를 이어 주는 것과 필요를 선언하는 것은 달라요. `tr.network_attached` 같은 트레이트를 붙이면 hard 필요를 선언하고 빈 채널을 만들어요. 끝이 any인 바인딩으로 할지, 필요를 선언하지 않는 새 트레이트로 할지는 미결 사항이에요. 관리망의 규칙도 더해요. 관리 경로가 없는 BMC를 표현할 수 있어야 해요.

### 그룹이 장비에 속하는 `PART_OF`

hard만 허용하는 아래 여섯 바인딩은 `whole_needs_part`의 기본값을 `none`으로 하고 허용 값을 hard와 none으로 해요. soft를 허용할지는 미결 사항이에요.

`RedundancyGroup`→`PhysicalServer`, `RedundancyGroup`→`Chassis`, `RedundancyGroup`→`NetworkDevice`, `RedundancyGroup`→`StorageArray`, `HaPair`→`StorageArray`, `RaidGroup`→`StorageArray`.

지금은 hard라서 `supplied_by` 링크가 없는 HA 쌍이나 RAID 그룹이 내려가면 장비 전체가 내려가요. 바꾸면 장비는 정상으로 남고 그 그룹이 만든 볼륨과 데이터스토어가 내려가요.

## 4. 점대점 연결

뜻은 링크 타입이 아니라 **바인딩 단위**로 켜요. 대상은 네트워크의 포트와 포트뿐이에요. 장비와 장비의 이웃 관계와 전원에는 켜지 않아요. 인자의 이름은 구현 때 정해요.

- 두 끝은 서로를 필요로 해요. 순환은 최소 고정점이 풀어요.
- 상대가 둘 이상이면(브레이크아웃, 대장과 이웃 데이터가 어긋남) 알 수 없음이에요. 서로 다른 상대의 수로 세요. 같은 상대를 가리키는 `CABLED_TO`와 `ADJACENT_TO`는 하나로 세요.
- 중간에 케이블 객체가 있으면 포트는 케이블을 필요로 하고 케이블은 양쪽 끝을 필요로 해요. 두 끝이 모두 네트워크 포트일 때만 적용하고 전원 케이블에는 적용하지 않아요.
- 패치 포트는 통과로 봐요. 먼 쪽을 찾는 방법은 미결 사항이에요.
- 포트에서 장비 전체로 번지지 않아야 해요. 장비의 일부인 `Lag`의 기본값이 none이 되는 것(3절)이 그 장치예요.
- `as_designed`는 관측한 이웃을 쓰지 않으므로 이 뜻이 적용되지 않아요. 한계예요.

### 바인딩 표 (`links.json`을 읽어 만든 것)

| 바인딩 | 켬/끔 | 이유 |
|---|---|---|
| `CABLED_TO` `Interface`↔`Interface` | 켬 | 네트워크 포트와 포트예요. |
| `CABLED_TO` `Interface`→`PatchPort` | 보류 | 패치 포트의 먼 쪽을 찾는 방법이 미정이에요. |
| `CABLED_TO` `PatchPort`↔`PatchPort` | 보류 | 같은 이유예요. |
| `ADJACENT_TO` `Interface`↔`Interface` | 켬 | 포트와 포트의 이웃 관계예요. |
| `ADJACENT_TO` `NetworkDevice`↔`NetworkDevice` | 끔 | 장비와 장비의 이웃 관계라서 포트의 대응이 없어요. |
| `TERMINATES_AT` `Cable`→`Interface` | 켬 | 케이블의 모든 끝이 네트워크 포트일 때만이에요. |
| `TERMINATES_AT` `Cable`→`PatchPort` | 보류 | 패치 포트의 통과 규칙이 정해지기 전이에요. |
| `TERMINATES_AT` `Cable`→`PduOutlet` | 끔 | 전원이에요. |
| `TERMINATES_AT` `Cable`→`PowerSupply` | 끔 | 전원이에요. |
| `TERMINATES_AT` `Cable`→`PowerFeed` | 끔 | 전원이에요. |
| `TERMINATES_AT` `Circuit`→`Interface` | 끔 | 회선은 포트가 아니에요. 회선이 끊기는 것은 그룹의 구성원으로 표현해요. |

집계: 켬 3, 끔 5, 보류 3(모두 11).

## 5. 이중화 상실의 계산

### 5.1 정의

- 그룹의 여유는 정상 구성원 수 − `min_alive`예요. 여유가 0인 그룹마다 "이 그룹이 내려간다"고 가정해요. 그 때문에 내려가는 것이 이중화 상실이에요.
- 그룹마다 따로 계산하지 않아요. 그룹이 내려가는 것을 원인으로 두고 원인의 표시를 달아서 지금의 장애 전파 경로(`part_of`, 그룹의 수, `managed_by` 포함)를 따라 한 번에 전파해요. 비용은 그래프 크기에 비례해요.
- 원인이 하나이면 확실한 상실이에요. 그룹에 직접 의존하면 "직접", 아니면 "간접"이에요. 서로 다른 원인이 함께 있어야 내려가면 "이중화 상실 가능"이에요. 결과 코드의 이름은 구현 때 정해요.
- 새 결과는 지금 답과의 합집합이에요. 페일오버를 선언한 사용자를 표시하는 지금의 규칙을 그대로 둬요. 그룹이 내려가도 페일오버 호스트는 내려가지 않기 때문이에요.
- 부품은 항목으로 나열하지 않고 장비 항목의 수로 합쳐요. 부품의 기준은 타입 이름을 읽지 않고 정해야 하고 미결 사항이에요.

아래 세 그래프는 지금 코드와 새 정의의 답을 나란히 놓은 손 계산이에요. **"지금 코드" 열은 코드를 읽은 판단이고, 새 테스트를 돌려서 얻은 것이 아니에요.** 새 정의의 열은 설계이고 구현되지 않았어요.

### 5.2 예 1: 한 원인으로 내려가는 경우

그래프. PDU-A와 PDU-B가 있어요. PSU-1은 PDU-A에서, PSU-2는 PDU-B에서 `POWERED_BY`로 전원을 받아요. PG는 `PowerRedundancyGroup`(`min_alive` 1)이고 PSU-1과 PSU-2가 구성원이에요. ARR(`StorageArray`)은 PG에서 `POWERED_BY`를 받고, OST(`ObjectStore`)는 ARR에서 `RUNS_ON`이고, BKT(`ObjectBucket`)는 OST의 `PART_OF`예요. 이 바인딩은 모두 있어요. 시드는 PDU-A가 내려간 것이에요.

| 대상 | 지금 코드(코드를 읽은 판단) | 새 정의(설계) |
|---|---|---|
| PSU-1 | 내려감 | 내려감 |
| PG | 정상(a=1, `min_alive`와 같아서 여유 0) | 여유 0이라 원인이 돼요 |
| ARR | exposed_direct | 직접, 원인 PG, 확실 |
| OST | exposed_inherited | 간접, 원인 PG, 확실 |
| BKT | 표시 없음. `part_of`는 노출됨을 전파하지 않아요. | 간접, 원인 PG, 확실. 별도 항목인지 OST 항목에 합치는지는 부품의 기준(미결)에 달려요. |

### 5.3 예 2: 두 원인이 함께 필요한 경우

그래프. 서버 A와 B가 그룹 H(`min_alive` 1)의 구성원이고 서비스 svc가 H에 `RUNS_ON`이에요(일반 규칙 3으로 통과해요). A는 G1에서, B는 G2에서 `POWERED_BY`를 받아요. G1과 G2는 각각 PSU 둘(`min_alive` 1)이에요. 시드는 PDU1a와 PDU2a가 내려간 것이에요.

| 대상 | 지금 코드(코드를 읽은 판단) | 새 정의(설계) |
|---|---|---|
| A | exposed_direct | 직접, 원인 G1, 확실 |
| B | exposed_direct | 직접, 원인 G2, 확실 |
| H | 변화 없음(a=2) | 이중화 상실 가능(원인 G1과 G2가 함께 필요) |
| svc | 표시 없음 | 이중화 상실 가능(원인 G1과 G2) |

### 5.4 예 3: 페일오버 그룹의 지금 답이 지켜지는 경우

그래프. 호스트 h1, h2, h3와 클러스터 C(`min_alive` 2)가 있어요. vm1은 h1에, vm2는 h2에 `RUNS_ON`이고 둘 다 `failover_to`가 C예요. app은 vm2에 `RUNS_ON`이에요. 시드는 h1이 내려간 것이에요.

| 대상 | 지금 코드(코드를 읽은 판단) | 새 정의(설계) |
|---|---|---|
| vm1 | interrupted | interrupted |
| vm2 | exposed_direct(페일오버로 C를 쓰는 사용자) | exposed_direct. 합집합이라 유지해요. |
| app | exposed_inherited | exposed_inherited |

C가 내려간다고 가정해도 페일오버 호스트는 내려가지 않아서 새 규칙만으로는 vm2가 표시되지 않아요. 그래서 지금의 규칙을 합집합으로 남겨요.

### 5.5 찾지 못하는 경우

여유는 남았지만 남은 구성원이 한 설비에 몰린 경우는 `impact`도 `exposure`도 찾지 못해요. 다른 질의가 다룬다는 말은 하지 않아요. 공통 설비에 몰린 구성원을 찾는 일은 뒤로 미뤘어요([14절의 한계](kg-queries.md)에도 적었어요).

### 5.6 일부 손실

분산 스토리지에서 허용한 수를 넘게 잃으면 "내려감"이 아니라 "알 수 없음(일부 손실)"이에요. 그룹 계산 방식을 하나 더해요(이름은 구현 때). 결과 종류는 `unknown`이고 이유를 붙여요. 새 결과 종류로 만들지는 구현 설계에서 정해요.

## 6. 조사에서 거부된 연결과 처리

두 조사 보고서에서 모은 것이에요. 보고서에 없는 연결은 넣지 않았어요. "지금"은 현재 `links.json` 기준이에요.

| 연결 | 처리 | 이유 |
|---|---|---|
| `PhysicalServer`, `HypervisorHost`, `ClusterNode`가 `HaPair`의 구성원 | 일반 규칙 1(알림) | 구성원은 누구나 될 수 있어요. 흔한 것은 알려진 조합으로도 올려요. |
| `ArrayController`가 `QuorumGroup`의 구성원 | 일반 규칙 1 | 위와 같아요. |
| `HaPair`가 `HaPair`나 `QuorumGroup`의 구성원 | 일반 규칙 2 | 그룹이 그룹의 구성원이에요. |
| `NetworkDevice`, `Router`가 `EcmpGroup`의 구성원 | 일반 규칙 1 | |
| `LineCard`, `LoadBalancerAppliance`가 `HaPair`의 구성원 | 일반 규칙 1 | |
| `Circuit`이 `EcmpGroup`의 구성원 | 일반 규칙 1 | |
| `PhysicalServer`가 `RaidGroup`의 구성원 | 일반 규칙 1 | |
| `Datastore` BACKED_BY `ReplicationGroup`, `RaidGroup` | 일반 규칙 3 | 구성원에게 의존할 수 있는 곳이에요. |
| `VirtualDisk`, `StoragePool`, `Endpoint` RUNS_ON `ReplicationGroup` | 일반 규칙 3 | |
| `Endpoint`, `StoragePool` RUNS_ON `HaPair` | 일반 규칙 3 | |
| `LbVirtualServer` RUNS_ON `HaPair` | 일반 규칙 3 | |
| `PhysicalServer`, `HypervisorHost` UPLINKED_VIA `HaPair`, `VirtualChassis` | 일반 규칙 3 | |
| `StorageArray` UPLINKED_VIA `HaPair` | 일반 규칙 3 | |
| `NetworkDevice`, `HypervisorHost` MANAGED_BY `QuorumGroup` | 일반 규칙 3 | |
| `DnsZone` RUNS_ON `HaPair` | 일반 규칙 3 | |
| `DnsZone` RUNS_ON `PhysicalServer`(여러 서버) | 여전히 거부 | 카디널리티가 N:1이에요. 그룹의 형태(`HaPair`)가 대신해요. |
| `StoragePool` BACKED_BY `StorageVolume` | 알려진 조합으로 올림 | 조사한 구성에서 흔해요. |
| `VirtualDisk` BACKED_BY `StorageVolume`, `StoragePool` | 알려진 조합으로 올림 | |
| `Chassis` PART_OF `StorageArray` | 알려진 조합으로 올림 | |
| `PowerSupply` PART_OF `StorageArray` | 알려진 조합으로 올림 | ADR-0057이 한계로 남긴 것이에요. |
| `Endpoint`의 `ObjectStore`, `FileShare` 링크 | 알려진 조합으로 올림 | |
| 볼륨, 디스크, 공유, 오브젝트 스토어 MANAGED_BY 관리 서비스 | 알려진 조합으로 올림 | |
| `Bmc` UPLINKED_VIA `NetworkDevice` | 알려진 조합으로 올림 | 관리망의 규칙이에요. |
| `VirtualMachine`→`VirtualNetwork`, `VirtualNetwork`→`VirtualSwitch`, `VirtualSwitch`→`HypervisorHost`의 UPLINKED_VIA | 알려진 조합으로 올림 | |
| `ClusterNode` UPLINKED_VIA `NetworkDevice`, `HypervisorHost` | 알려진 조합으로 올림 | 연결 경로예요. |
| `Router` RUNS_ON `VirtualMachine` | 알려진 조합으로 올림(보류) | 변형 실험은 하지 않았어요. |
| `PhysicalServer`, `OperatingSystem`, `VirtualMachine`의 DEPENDS_ON | 알려진 조합으로 올림 | 강도는 링크마다 써요. |
| `Interface` RUNS_ON `Cable`, `PatchPort`, `Circuit` | 새 바인딩 없음 | 4절의 `TERMINATES_AT`의 점대점이 같은 답을 줘요. |
| `Interface` UPLINKED_VIA `SanSwitch` | 여전히 거부 | SAN 경로는 결정이 없어요. 존, 마스킹, 멀티패스를 의존 대상으로 삼는 것도 같아요. |
| `PhysicalServer` LOCATED_IN `Site` | 여전히 거부 | 결정이 없어요. |
| `OperatingSystem`, 서버의 `DnsZone`, `OverlaySegment` 의존 | 여전히 거부 | 결정이 없어요. |
| 한 `SoftwareInstance`가 여러 그룹에 속함 | 여전히 거부 | N:1 알려진 조합(`ha_pair`, `quorum_group`, `redundancy_group`)이 우선순위로 이겨서 같은 바인딩의 두 번째 그룹은 거부돼요. 미결 사항이에요. |
| 오버레이 터널을 AND로 묶는 것 | 여전히 거부 | 결정이 없어요. |

지금도 통과하는 것: `Lag` MEMBER_OF `EcmpGroup`, `StorageArray` UPLINKED_VIA `Lag`, `VirtualSwitch` PART_OF `HypervisorHost`, `NetworkDevice` MANAGED_BY `Bmc`.

## 7. 조사의 한계

- 구성 25가지(저장 13, 네트워크 12), 장애 질문 약 163개(저장 75, 네트워크 88), 틀린 답 58건(각 29건)이에요. 숫자는 대략이에요.
- 타입별 계산이 필요한 경우는 두 쪽 모두 0건이에요.
- 판정은 조사자의 일반 지식으로 내렸어요. 검토가 공개 문서로 맞춘 것은 스케일아웃 NAS의 보호 수준, 분산 스토리지의 최소 복제 수, 하이퍼컨버지드의 과반 규칙, Redfish의 이중화 스키마, NetBox의 케이블이에요. 약 160개 질문은 하나하나 맞추지 않았어요.
- 그래서 "실제 답"을 공개 문서로 확인한 뒤에야 테스트를 잠가요(8절의 순서 3).

## 8. 답이 바뀌는 변경과 순서

| 순서 | 변경 | 답 |
|---|---|---|
| 0단계 | 링크별 인자, 값의 순서, 우선순위 | 영향 범위의 답은 같아요. 수집의 검사는 바뀔 수 있어요. |
| 1 | 알려진 조합과 일반 규칙, 알림 | 영향 범위는 같아요. 거부되던 링크가 들어와요. |
| 2 | 실제 답을 공개 문서로 확인 | 테스트를 잠그기 전이에요. |
| 3 | 한 릴리스: 점대점, 이중화 상실의 재정의, 일부 손실, ECMP `min_alive` 바로잡기, 그룹이 장비에 속하는 `PART_OF`의 기본값 | 답이 바뀌어요. 시나리오 테스트는 변경과 같은 PR에 써요. |
| 4 | 구성 가이드 | 확인된 구성만이에요. |
| 5 | 합성 세계의 다양화, 관리 도구 소스 | 인스턴스를 확인한 뒤예요. |

### 합성 세계에서 달라질 것으로 예상되는 답 (모두 추정)

- 서버 NIC↔ToR, ToR↔집계, 집계↔스파인, 컨트롤러↔ToR의 `CABLED_TO` `Interface`↔`Interface`에 점대점이 켜지면 `Lag`와 ECMP의 수가 반대쪽에 반응해요. 지금은 스파인이 모두 내려가도 집계는 영향이 없지만 바뀐 뒤에는 네트워크를 잃어요. 반대쪽 포트가 목록에 "내려감"으로 나와요.
- ECMP의 `min_alive`가 1이 되어 용량이 줄면 degraded나 exposed로 보여요.
- 그룹이 장비에 속하는 `PART_OF`의 기본값 none은 `supplied_by` 링크가 없는 곳에서만 답을 바꿔요. `HaPair`→어레이와 `RaidGroup`→어레이예요. 지금은 어레이 전체가 내려가지만 바뀐 뒤에는 어레이가 정상으로 남고 그 쌍이나 RAID 그룹이 만든 볼륨과 데이터스토어가 내려가요. 전원 장치, 팬, ToR의 `Lag`, ECMP는 이미 `supplied_by` 링크가 있어요.
- 부품을 합치지 않으면 서버마다 항목이 15개 넘게 늘어난다는 것이 검토의 추정이에요.
- 골든 세계의 해시가 ECMP 값 때문에 바뀌어요.
- 고쳐야 할 것으로 예상되는 테스트(구현하는 결정이 고쳐요): `backend/internal/worldgen/golden_test.go`, `backend/internal/model/standard_test.go`의 해시와 개수 테스트, `backend/internal/readcheck/impact_scenarios_test.go`와 `impact_scenarios2_test.go`, `backend/internal/query/impact_*_test.go`.

## 9. 가상화 관리 도구를 소스로 쓸 때

- 확인된 것(공개 문서): 어댑터가 선언하는 관계는 부모/자식뿐이고 의미 이름이 없어요. 수집 주기는 기본 5분이에요. 값은 마지막으로 수집한 값과 시각이에요. 인벤토리의 변경 알림이 없어요. 물리 NIC, 가상 NIC, 가상 디스크는 이름 붙은 속성 묶음으로 와요.
- 확인되지 않은 것(추정): 호스트별 분산 스위치의 업링크, 부모 사슬 전체, 식별자의 내용.
- 관계 매핑 표는 소스 정의에 두고 (어댑터 종류, 부모 리소스 종류, 자식 리소스 종류)를 링크 타입과 방향에 이어요. 표에 없는 쌍은 문제 목록에 쌍마다 한 줄 남겨요.
- 표와 추가할 타입의 목록은 실제 인스턴스에서 확인되기 전에는 초안이에요. 공개 리포지토리에는 확인된 것만 둬요.
- 호스트의 유지보수 속성은 이미 있어요(`tr.schedulable`의 `maintenance_state`).

## 10. 미결 사항

ADR-0059의 미결 사항과 같아요. 필요를 선언하지 않는 경로 트레이트, "장애가 날 수 있는" 타입의 트레이트, 그룹 순환 검사의 위치, 부품의 기준, "이중화 상실 가능"의 결과 종류와 표시, 죽은 반대쪽 포트의 표시, 패치 포트와 보류 세 건, 그룹이 장비에 속하는 `PART_OF`의 soft, 기본값이 바뀔 때의 점검, `SoftwareInstance`의 N:1, 카디널리티를 좁히는 시점, 임계가 둘인 분산 스토리지의 `min_alive`, 속성 묶음의 매핑, 순환이 있을 때 상류 질의의 경로 모양, SAN 경로와 `LOCATED_IN` `Site`와 `OverlaySegment` 의존이에요.
