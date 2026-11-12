---
title: 표준 모델의 요약 — 층 아홉의 타입, 링크, 액션
kind: reference
status: active
follows:
  - docs/design-docs/adr/0007-kg-model.md
---
# 표준 모델의 요약

**이 문서는 요약이에요.** 표준 모델의 실체는 다음 변경에서 코드와 함께 데이터 파일로 실어요. 여기서는 층별 요약과 대표 타입, 규칙만 적어요. 모델의 칸과 규칙은 [KG의 모델](kg-model.md), 결정은 [ADR-0007](adr/0007-kg-model.md)이에요.

- **타입 수는 조사 담당의 초안의 수예요.** 객체 타입 168개(핵심 122, 확장 46), 링크 타입 이름 58개와 바인딩 157개, 액션 타입 112개. 아래로 옮겨 적으면서 이름과 수가 조금 달라질 수 있고, 데이터 파일로 실을 때 굳어요.
- **핵심**은 영향 범위의 계산에 필요한 타입이고 **확장**은 소스가 흔히 주거나 운영에 흔히 필요한 타입이에요. 표의 타입은 모두 핵심이고, 확장 타입은 표 아래에 이름만 적어요.
- **빌린 표준**에는 조사로 확인한 것만 표준 이름을 적어요. 확인하지 못한 것은 "프락시움이 정함"이에요. 표준에서는 이름, 골격, 식별자, 고르기 값만 빌리고 링크의 뜻, 번짐, 근거, 시간은 프락시움이 정해요. 223P(건물 설비의 개념 모델)는 승인되지 않은 초안이라 개념만 참고해요.
- 모든 항목의 이름 공간은 `std`예요. 링크의 뜻은 5절의 열하나, 액션의 축은 `mutates`·`reversibility`·`disruption`·`physical` 순이에요(kg-model의 12절).

## 1. 초안을 이 모델에 맞게 옮긴 규칙

| 초안의 모양 | 이 모델에서 |
|---|---|
| 부품과 전체의 `INSTALLED_IN`과 `REQUIRES_PART`, 롤업 정책 | `PART_OF`(`part_of`) 하나. 전체가 부품을 필요로 하는지는 `whole_needs_part`. 이중화하는 부품은 `none`이고 묶음을 거쳐요 |
| 자리의 `contained_in` | `LOCATED_IN`, `MOUNTED_IN`만. 부품에는 쓰지 않아요 |
| 번지지 않는 논리 구획(`contained_in`, 번짐 없음) | `LogicalGroup`의 하위 타입과 `MEMBER_OF` (`VirtualDatacenter`, `Namespace`, `Environment`, `Team`, `PlacementRule` 등) |
| 위험 등급 다섯 | 축 넷. 읽기는 `mutates`가 아니요, 되돌리는 액션이 있으면 `reversible`, 데이터를 덮거나 지우면 `data_loss`, 현장 작업이면 `physical` |
| 구성에서만 끌어낸 묶음 | 묶음마다 설정·표·문서의 길을 받아요(8절) |
| 신호 하나인 상태 | 채널 여섯. `POWERED_BY`는 `supplied_by(power)`, `COOLED_BY`는 `supplied_by(cooling)`, `UPLINKED_VIA`는 `supplied_by(network)`, `BACKED_BY`는 `supplied_by(storage)` |

## 2. 자리와 설비
전원 사슬(인입에서 콘센트까지)과 냉방(공조, 냉각수 루프), 자리의 위아래를 덮어요.

| 이름 | 뜻 | 빌린 표준 |
|---|---|---|
| `Site` | 부지, 캠퍼스 | Redfish `Facility`(Site) |
| `Building` | 건물 | Redfish `Facility`(Building) |
| `Floor` | 층 | Redfish `Facility`(Floor) |
| `Room` | 실(데이터홀, 전기실, 기계실) | Redfish `Facility`(Room) |
| `Row` | 랙 열 | Redfish `ChassisType`의 `Row` |
| `Rack` | 랙 | Redfish `ChassisType`의 `Rack` |
| `UtilityFeed` | 전력회사 인입 | 프락시움이 정함 |
| `Generator` | 비상 발전기 | 프락시움이 정함 |
| `Transformer` | 변압기 | 프락시움이 정함(223P 개념 참고) |
| `Switchgear` | 수배전반 | Redfish `PowerDistribution`의 `Switchgear` |
| `TransferSwitch` | 절체 스위치 | Redfish의 `AutomaticTransferSwitch`, `ManualTransferSwitch` |
| `Ups` | 무정전 전원장치 | 프락시움이 정함(Redfish에 UPS 유형이 없어요) |
| `BatteryBank` | 배터리 뱅크 | Redfish의 `BatteryShelf` |
| `PowerPanel` | 분전반 | NetBox `PowerPanel` |
| `Rpp` | 원격 전원 패널 | 프락시움이 정함 |
| `FloorPdu` | 플로어 PDU | Redfish `FloorPDU` |
| `RackPdu` | 랙 PDU | Redfish `RackPDU` |
| `PduOutlet` | PDU 콘센트 | Redfish `PowerDistribution`의 `Outlets` |
| `PowerCircuit` | 분기 회로 | Redfish의 `Branches`, `Feeders`, `Subfeeds` |
| `PowerFeed` | 급전선(A·B 측) | NetBox `PowerFeed` |
| `Chiller` | 칠러 | 프락시움이 정함(223P 개념 참고) |
| `CoolingTower` | 냉각탑 | 프락시움이 정함(223P 개념 참고) |
| `Crah` | 냉수식 공조기 | 프락시움이 정함 |
| `Crac` | 직팽식 공조기 | 프락시움이 정함 |
| `InRowCooler` | 열간 쿨러 | 프락시움이 정함 |
| `Cdu` | 냉각수 분배 장치 | Redfish `CoolingUnit`의 `CDU` |
| `CoolingLoop` | 냉각 루프 | Redfish `Facility`의 `CoolingLoops` 링크 |
| `Containment` | 통로 격리 | 프락시움이 정함 |

확장: `Region`, `RoomZone`, `Aisle`, `RackUnit`, `FuelTank`, `Busway`, `CircuitBreaker`, `PowerShelf`, `DryCooler`, `RearDoorCooler`, `HeatExchanger`, `ImmersionTank`, `FireSuppressionSystem`.

| 링크 | 출발 → 도착 | 뜻 |
|---|---|---|
| `LOCATED_IN` | `Building` → `Site`, `Floor` → `Building`, `Room` → `Floor`, `Rack` → `Row`, 설비 → `Room` | `contained_in` |
| `MOUNTED_IN` | 랙에 장착되는 것(`~RackMountable`) → `Rack` | `contained_in` |
| `POWERED_BY` | `~Powered` → `~PowerSource` 또는 `PowerRedundancyGroup` (예: `RackPdu` → `PowerFeed`) | `supplied_by(power)` |
| `COOLED_BY` | `~Cooled` → `~CoolingSource` 또는 `CoolingRedundancyGroup` | `supplied_by(cooling)` |
| `PART_OF` | `PduOutlet` → `RackPdu`, `BatteryBank` → `Ups` | `part_of` |

| 액션 | 대상 | 축 넷 |
|---|---|---|
| `set_cooling_setpoint` | `Crah`, `Crac`, `Chiller` | 예 · reversible · none · 아니요 |
| `open_breaker` | `CircuitBreaker`, `PowerCircuit` | 예 · reversible · service_affecting · 예 |

## 3. 물리 서버와 부품
서버, 섀시, 부품, 관리 컨트롤러를 덮어요.

| 이름 | 뜻 | 빌린 표준 |
|---|---|---|
| `Chassis` | 물리 섀시(선택) | Redfish `Chassis` |
| `BladeEnclosure` | 블레이드와 슬레드를 담는 섀시 | Redfish `ChassisType`의 `Enclosure`, `Blade` |
| `PhysicalServer` | OS가 올라가는 하드웨어 하나 | Redfish `ComputerSystem`, OpenTelemetry `host`, NetBox `Device` |
| `Cpu` | 프로세서(소켓 단위) | OpenConfig 컴포넌트 `CPU`, OpenTelemetry `host.cpu` |
| `MemoryModule` | 메모리 모듈 | 프락시움이 정함 |
| `Drive` | 디스크, SSD, NVMe | OpenConfig 컴포넌트 `STORAGE` |
| `StorageController` | 서버 안의 RAID, HBA 컨트롤러 | 프락시움이 정함 |
| `NetworkAdapter` | NIC, HBA, DPU 카드 | Redfish `NetworkAdapter` |
| `Accelerator` | GPU, FPGA, NPU | 프락시움이 정함 |
| `PowerSupply` | 장비 전원 모듈 | OpenConfig 컴포넌트 `POWER_SUPPLY` |
| `Fan` | 팬, 팬 트레이 | OpenConfig 컴포넌트 `FAN` |
| `Bmc` | 대역외 관리 컨트롤러 | 프락시움이 정함 |

확장: `ChassisBay`, `Baseboard`, `PcieDevice`, `Firmware`.

| 링크 | 출발 → 도착 | 뜻 |
|---|---|---|
| `PART_OF` | `Cpu` → `PhysicalServer`(`hard`), `MemoryModule`, `Drive` → `PhysicalServer`(`soft`), `PowerSupply`, `Fan` → `~HardwareAssembly`(`none`) | `part_of` |
| `MEMBER_OF` | `PowerSupply` → `PowerRedundancyGroup`, `Fan` → `RedundancyGroup` | `member_of` |
| `POWERED_BY` | `PhysicalServer` → `PowerRedundancyGroup` 또는 `PduOutlet` | `supplied_by(power)` |

| 액션 | 대상 | 축 넷 |
|---|---|---|
| `replace_component` | 부품 | 예 · irreversible · service_affecting · 예 |
| `update_firmware` | 서버, `Bmc`, `NetworkAdapter` | 예 · reversible · service_affecting · 아니요 |

## 4. 네트워크 (무선 제외)
장비, 포트, LAG, 라우팅 세션과 프로세스, 주소, VLAN, 패브릭, 외부 회선을 덮어요. 포트 단위까지 받아요.

| 이름 | 뜻 | 빌린 표준 |
|---|---|---|
| `NetworkDevice` | 네트워크 장비의 상위 | RFC 8345의 `node`, OpenConfig 플랫폼, NetBox `Device` |
| `Switch` | 스위치 | 프락시움이 정함 |
| `Router` | 라우터 | 프락시움이 정함 |
| `Firewall` | 방화벽 | 프락시움이 정함 |
| `LoadBalancerAppliance` | 부하 분산 장비 | 프락시움이 정함 |
| `Transceiver` | 광·구리 트랜시버 | OpenConfig 컴포넌트 `TRANSCEIVER` |
| `Interface` | L2·L3 종단(물리, 서브, 루프백 등) | RFC 8345의 `termination-point`, RFC 8944, NetBox `Interface` |
| `Cable` | 물리 케이블 | 프락시움이 정함 |
| `Lag` | 링크 어그리게이션(`RedundancyGroup`의 하위 타입) | OpenConfig `if-aggregate`의 `min-links`, RFC 8944의 `lag` |
| `MlagDomain` | 두 스위치가 하나로 보이는 쌍 | 프락시움이 정함 |
| `VirtualChassis` | 스택, 가상 섀시 | NetBox `VirtualChassis` |
| `Vlan` | VLAN | RFC 8944의 태그 속성 |
| `NetworkInstance` | 라우팅·포워딩 인스턴스 | 프락시움이 정함 |
| `Prefix` | IP 프리픽스 | RFC 8346의 `prefix` |
| `IpAddress` | IP 주소 | RFC 8346의 종단 주소 |
| `RoutingSession` | 라우팅 세션, 인접(BGP 이웃 등) | 프락시움이 정함 |
| `RoutingProcess` | 장비의 라우팅 프로세스와 여러 경로 설정 | 프락시움이 정함 |
| `EcmpGroup` | 같은 비용 다중 경로의 묶음 | 프락시움이 정함 |
| `Tunnel` | 점대점 터널 | 프락시움이 정함 |
| `OverlaySegment` | 오버레이 세그먼트 | RFC 8944의 `vxlan/vni-id` |
| `Fabric` | 패브릭, 네트워크 도메인 | RFC 8345의 `network` |
| `Circuit` | 외부 회선 | 프락시움이 정함 |

확장: `LineCard`, `PatchPanel`, `PatchPort`, `StaticRoute`, `FabricPod`, `AutonomousSystem`.

묶음을 받는 길마다 타입과 속성이 있어요. 설정에서는 `RoutingProcess`(`maximum_paths`)와 `RoutingSession`(`multipath_eligible`), `Lag`(`configured_min_links`, `member_ports_config`)가 받아요. 표에서는 이웃 표가 `RoutingSession`의 `session_state`를, 경로표가 `EcmpGroup`의 구성원을 받아요. 문서(설계서, 엑셀)는 같은 타입을 `declared`로 받아요. 길은 근거의 종류가 말해요.

| 링크 | 출발 → 도착 | 뜻 |
|---|---|---|
| `PART_OF` | `Interface` → `NetworkDevice`(`none`), `Transceiver` → `Interface`(`hard`) | `part_of` |
| `MEMBER_OF` | `Interface` → `Lag`, `RoutingSession` → `EcmpGroup`, `NetworkDevice` → `MlagDomain` | `member_of` |
| `CABLED_TO` | `Interface` ↔ `Interface` (`Cable`에서 끌어내요) | `connected_to` |
| `UPLINKED_VIA` | `NetworkDevice` → `EcmpGroup`, `Lag`, `Interface` | `supplied_by(network)` |
| `PEERS_VIA` | `RoutingSession` → `IpAddress` | `requires`(`hard`) |

| 액션 | 대상 | 축 넷 |
|---|---|---|
| `disable_interface` | `Interface` | 예 · reversible · service_affecting · 아니요 |
| `set_lag_min_links` | `Lag` | 예 · reversible · service_affecting · 아니요 |

## 5. 스토리지
어레이, 풀, 볼륨, 파일 공유, SAN 스위치를 덮어요.

| 이름 | 뜻 | 빌린 표준 |
|---|---|---|
| `StorageArray` | 스토리지 시스템 | Swordfish `StorageService` |
| `ArrayController` | 어레이 컨트롤러, 노드 | Swordfish의 스토리지 컨트롤러 |
| `StoragePool` | 용량 풀 | Swordfish `StoragePool` |
| `RaidGroup` | RAID, 이레이저 코딩 그룹(`RedundancyGroup`의 하위 타입) | Redfish `VolumeType` 열거 |
| `StorageVolume` | 볼륨, LUN, 네임스페이스 | Swordfish `Volume`, Redfish `Volume` |
| `FileSystem` | 파일시스템 | Swordfish `FileSystem` |
| `FileShare` | NFS, SMB 공유 | 프락시움이 정함 |
| `ObjectBucket` | 오브젝트 버킷 | 프락시움이 정함 |
| `SanSwitch` | SAN(FC) 스위치 | 프락시움이 정함 |

확장: `InitiatorGroup`, `ObjectStore`, `SanZone`, `StorageSnapshot`, `ReplicationGroup`.

| 링크 | 출발 → 도착 | 뜻 |
|---|---|---|
| `BACKED_BY` | `StorageVolume` → `StoragePool`, `Datastore` → `StorageVolume` | `supplied_by(storage)` |
| `EXPORTED_TO` | `StorageVolume` → `InitiatorGroup`, `HypervisorHost` | `connected_to` |
| `MEMBER_OF` | `Drive` → `RaidGroup` | `member_of` |

| 액션 | 대상 | 축 넷 |
|---|---|---|
| `extend_volume` | `StorageVolume` | 예 · irreversible · none · 아니요 |
| `delete_volume` | `StorageVolume` | 예 · irreversible · data_loss · 아니요 |

## 6. 가상화
관리 서버, 호스트, 클러스터, VM, 가상 디스크와 NIC, 가상 스위치, 데이터스토어를 덮어요. 가상화 플랫폼 둘 이상을 받는 중립 이름이에요.

| 이름 | 뜻 | 빌린 표준 |
|---|---|---|
| `VirtualizationManager` | 가상화 관리 서버 | 프락시움이 정함 |
| `VirtualDatacenter` | 관리 범위(시설의 `Site`와 무관, `LogicalGroup`의 하위 타입) | 프락시움이 정함 |
| `ComputeCluster` | 호스트 풀, HA 단위(`RedundancyGroup`의 하위 타입) | 프락시움이 정함 |
| `HypervisorHost` | 하이퍼바이저가 설치된 호스트 | 프락시움이 정함 |
| `VirtualMachine` | 가상 머신 | libvirt의 `domain`(표면 수준) |
| `VirtualDisk` | 가상 디스크 | libvirt의 `devices/disk`(표면 수준) |
| `VirtualNic` | 가상 NIC | libvirt의 `devices/interface`(표면 수준) |
| `VirtualSwitch` | 가상 스위치 | 프락시움이 정함 |
| `VirtualNetwork` | 가상 네트워크 세그먼트 | 프락시움이 정함 |
| `Datastore` | VM 디스크가 놓이는 저장 단위 | 프락시움이 정함 |
| `PlacementRule` | 배치, 친화 규칙(`LogicalGroup`의 하위 타입) | 프락시움이 정함 |

확장: `ResourcePool`, `VmTemplate`, `VmSnapshot`, `HostAggregate`.

| 링크 | 출발 → 도착 | 뜻 |
|---|---|---|
| `RUNS_ON` | `VirtualMachine` → `HypervisorHost`(`failover_to`: `ComputeCluster`), `HypervisorHost` → `PhysicalServer` | `runs_on` |
| `PART_OF` | `VirtualDisk`, `VirtualNic` → `VirtualMachine` | `part_of` |
| `MEMBER_OF` | `HypervisorHost` → `ComputeCluster` | `member_of` |
| `BACKED_BY` | `VirtualDisk` → `Datastore` | `supplied_by(storage)` |

| 액션 | 대상 | 축 넷 |
|---|---|---|
| `migrate_virtual_machine` | `VirtualMachine` | 예 · reversible · none · 아니요 |
| `delete_virtual_machine` | `VirtualMachine` | 예 · irreversible · data_loss · 아니요 |

## 7. 컨테이너
클러스터, 노드, 네임스페이스, 워크로드, 파드, 서비스, 진입점, 영속 볼륨을 덮어요.

| 이름 | 뜻 | 빌린 표준 |
|---|---|---|
| `ContainerCluster` | 컨테이너 오케스트레이션 클러스터 | OpenTelemetry `k8s.cluster` |
| `ControlPlane` | 제어면(정족수 묶음) | 프락시움이 정함 |
| `ClusterNode` | 워커, 컨트롤 노드 | OpenTelemetry `k8s.node` |
| `Namespace` | 네임스페이스(`LogicalGroup`의 하위 타입) | OpenTelemetry `k8s.namespace` |
| `Workload` | 배포, 스테이트풀셋 등의 추상(복제 묶음) | OpenTelemetry의 `deployment`, `statefulset`, `daemonset`, `job`, `cronjob` |
| `Pod` | 파드 | OpenTelemetry `k8s.pod` |
| `ClusterService` | 안정된 주소와 부하 분산 | 프락시움이 정함 |
| `Gateway` | 클러스터 진입점 | Gateway API `Gateway` |
| `GatewayRoute` | 라우팅 규칙 | Gateway API의 `HTTPRoute` 등 Route |
| `PersistentVolume` | 영속 볼륨 | OpenTelemetry `k8s.persistentvolume` |
| `PersistentVolumeClaim` | 영속 볼륨 클레임 | OpenTelemetry `k8s.persistentvolumeclaim` |
| `StorageClass` | 스토리지 클래스 | 프락시움이 정함 |

확장: `NodePool`, `Container`, `ContainerImage`, `DisruptionBudget`.

| 링크 | 출발 → 도착 | 뜻 |
|---|---|---|
| `RUNS_ON` | `Pod` → `ClusterNode` | `runs_on` |
| `REALIZED_BY` | `ClusterNode` → `VirtualMachine`, `PhysicalServer` | `realized_by` |
| `MEMBER_OF` | `Pod` → `Workload`, `Workload` → `Namespace` | `member_of` |
| `BACKED_BY` | `PersistentVolume` → `StorageVolume` | `supplied_by(storage)` |

| 액션 | 대상 | 축 넷 |
|---|---|---|
| `scale_workload` | `Workload` | 예 · reversible · none · 아니요 |
| `delete_pod` | `Pod` | 예 · irreversible · service_affecting · 아니요 |

## 8. 플랫폼과 OS
OS, 미들웨어, 데이터베이스, 접속 종단, 부하 분산 설정, 이름 해석, 인증서를 덮어요.

| 이름 | 뜻 | 빌린 표준 |
|---|---|---|
| `OperatingSystem` | 실행 중인 OS | OpenTelemetry `host`(`host.id`) |
| `SoftwareInstance` | 미들웨어, 애플리케이션 런타임 | 프락시움이 정함 |
| `DatabaseInstance` | DB 엔진 인스턴스 | 프락시움이 정함 |
| `Endpoint` | 소비자가 접속하는 종단(주소, 포트, 프로토콜) | 프락시움이 정함 |
| `LbVirtualServer` | 부하 분산의 가상 서버(VIP와 포트) | 프락시움이 정함 |
| `LbPool` | 부하 분산의 백엔드 풀(묶음) | 프락시움이 정함 |
| `LbPoolMember` | 풀의 멤버 | 프락시움이 정함 |
| `DnsZone` | DNS 존 | 프락시움이 정함 |
| `DnsRecord` | DNS 레코드 | 프락시움이 정함 |
| `Certificate` | 인증서 | 프락시움이 정함 |

확장: `OsService`, `Database`, `CertificateAuthority`.

| 링크 | 출발 → 도착 | 뜻 |
|---|---|---|
| `RUNS_ON` | `OperatingSystem` → `VirtualMachine`, `PhysicalServer`. `SoftwareInstance` → `OperatingSystem` | `runs_on` |
| `REALIZED_BY` | `Endpoint` → `SoftwareInstance`, `IpAddress` | `realized_by` |
| `FORWARDS_TO` | `LbVirtualServer` → `LbPool` | `requires`(`hard`) |
| `MEMBER_OF` | `LbPoolMember` → `LbPool` | `member_of` |

| 액션 | 대상 | 축 넷 |
|---|---|---|
| `restart_service` | `SoftwareInstance`, `DatabaseInstance` | 예 · reversible · service_affecting · 아니요 |
| `restore_database` | `Database` | 예 · irreversible · data_loss · 아니요 |

## 9. 서비스와 조직
서비스 계층, 환경, 소유와 담당, 유지보수 창을 덮어요. 사람은 로그인 쪽의 불투명한 참조만 두고 개인 속성을 두지 않아요. 맡는 쪽은 조직(`Team`)이에요.

| 이름 | 뜻 | 빌린 표준 |
|---|---|---|
| `BusinessService` | 소비자 대상 서비스(단일 레벨) | CSDM `Business Service` |
| `TechnicalService` | 기반 서비스(컴퓨트, 스토리지, 네트워크) | CSDM `Technical Service` |
| `ApplicationService` | 배포된 애플리케이션 스택의 논리 표현(환경별) | CSDM `Application Service` |
| `ServiceOffering` | 가용성과 지원 시간 단위 | CSDM `Service Offering` |
| `Environment` | 배포 환경(`LogicalGroup`의 하위 타입) | CSDM의 환경(dev, test, prod) 구분 |
| `Tenant` | 소유와 격리의 경계 | 프락시움이 정함 |
| `Team` | 담당 조직(`LogicalGroup`의 하위 타입) | Backstage `Group` |
| `MaintenanceWindow` | 유지보수, 동결 창 | 프락시움이 정함 |

확장: `BusinessCapability`, `Application`, `Person`, `OnCallSchedule`, `Organization`, `Contract`.

| 링크 | 출발 → 도착 | 뜻 |
|---|---|---|
| `REALIZED_BY` | `BusinessService` → `ServiceOffering` → `ApplicationService` → `Workload`, `SoftwareInstance` | `realized_by` |
| `DEPENDS_ON` | 서비스 → 서비스, `DatabaseInstance`, `Endpoint` | `requires` |
| `MEMBER_OF` | `ApplicationService` → `Environment` | `member_of` |
| `OWNED_BY` | 아무 객체 → `Team`, `Tenant` | `owned_by` |

| 액션 | 대상 | 축 넷 |
|---|---|---|
| `schedule_maintenance` | `~Schedulable` | 예 · reversible · none · 아니요 |

## 10. 가로지르는 것
이중화 묶음, 논리 묶음, 장애 영역, 센서, 자리 표시 객체를 덮어요.

| 이름 | 뜻 | 빌린 표준 |
|---|---|---|
| `RedundancyGroup` | 이중화 묶음의 상위 | OpenConfig의 `min-links` 개념 |
| `LogicalGroup` | 번지지 않는 논리 묶음의 상위 | OpenStack의 호스트 aggregate, Backstage `Group` 개념 |
| `PowerRedundancyGroup` | 전력 이중화(서버 전원 장치, 급전 A·B) | Redfish `PowerSupplyRedundancy` 등의 속성 이름 |
| `CoolingRedundancyGroup` | 냉방 이중화 | Redfish `CoolingUnitRedundancy` 등의 속성 이름 |
| `HaPair` | 능동·대기 두 노드 | 프락시움이 정함 |
| `QuorumGroup` | 과반 정족수 | 프락시움이 정함 |
| `FailureDomain` | 논리 장애 영역(가용 영역, 전원·냉방 영역) | OpenStack의 availability zone |
| `Sensor` | 센서, 계측점 | OpenConfig 컴포넌트 `SENSOR` |
| `UnknownObject` | 참조됐으나 아직 못 본 객체의 자리 표시 | 프락시움이 정함 |

확장: `MetricSource`. `Sensor`의 값은 `volatile` 속성이고 시계열은 KG에 쌓지 않아요.

| 링크 | 출발 → 도착 | 뜻 |
|---|---|---|
| `MEMBER_OF` | 구성원 → `RedundancyGroup` 또는 `LogicalGroup` | `member_of` |
| `IN_FAILURE_DOMAIN` | 아무 객체 → `FailureDomain` | `contained_in` |
| `MEASURES` | `Sensor` → `~Sensed` | `record_only` |

| 액션 | 대상 | 축 넷 |
|---|---|---|
| `get_status` | `~Manageable` | 아니요 · — · none · 아니요 |
| `power_off` | `PhysicalServer`, `VirtualMachine` | 예 · reversible · service_affecting · 아니요 |

## 11. 층을 가로지르는 링크

| 링크 | 출발 → 도착 | 뜻 |
|---|---|---|
| `MOUNTED_IN` | `PhysicalServer` → `Rack` | `contained_in` |
| `POWERED_BY` | `PowerSupply` → `PduOutlet`, `PhysicalServer` → `PowerRedundancyGroup` | `supplied_by(power)` |
| `COOLED_BY` | `Rack` → `CoolingRedundancyGroup`. 서버는 자리를 따라 냉방을 물려받아요(끌어낸 링크) | `supplied_by(cooling)` |
| `RUNS_ON` | `HypervisorHost` → `PhysicalServer`, `VirtualMachine` → `HypervisorHost`, `OperatingSystem` → `VirtualMachine`, `Pod` → `ClusterNode` | `runs_on` |
| `REALIZED_BY` | `ClusterNode` → `VirtualMachine`, `Endpoint` → `SoftwareInstance`, `ApplicationService` → `Workload` | `realized_by` |
| `UPLINKED_VIA` | `PhysicalServer`, `HypervisorHost` → `Lag` | `supplied_by(network)` |
| `BACKED_BY` | `Datastore`, `PersistentVolume` → `StorageVolume` | `supplied_by(storage)` |
| `OWNED_BY`, `IN_FAILURE_DOMAIN` | 아무 객체 → `Team`, `FailureDomain` | `owned_by`, `contained_in` |

## 12. 이중화 묶음의 하위 타입과 끌어내는 곳

구성원과 문턱은 설정에서 끌어내고, 표나 문서에서도 받아요(8절의 길). 같은 묶음에 길이 여럿이면 종류별로 받아 두고 어긋남은 문제 목록에 올라요.

| 하위 타입 | 구성원 | 방식과 문턱 | 끌어내는 곳 |
|---|---|---|---|
| `PowerRedundancyGroup` | `PowerSupply`, 상류 급전 | `capacity` 또는 `count` | 서버가 말하는 전원 장치 목록과 이중화 설정, 급전선 표(엑셀) |
| `CoolingRedundancyGroup` | `Crah`, `Crac`, `Chiller`, `Cdu` | `capacity`(N+1) | 설비 목록과 구역 매핑 |
| `Lag` | `Interface` | `count`, `min_alive`는 최소 링크 | 집계 설정, 장비의 집계 상태 표 |
| `MlagDomain` | `NetworkDevice` 둘 | `count`, 1 | 양쪽이 대칭인 MLAG 설정 |
| `EcmpGroup` | `RoutingSession`, `StaticRoute`, `Lag`, `Interface` | `count`, 1 (`maximum_paths`가 상한) | 여러 경로 설정과 이웃, 경로표, 설계서 |
| `RaidGroup` | `Drive` | `count`, n − 패리티 | 컨트롤러, 어레이의 RAID 설정 |
| `ComputeCluster` | `HypervisorHost` | n − 넘겨받기 용량의 호스트 수 | 클러스터 HA 설정 |
| `Workload`, `ControlPlane`, `LbPool` | `Pod`, 제어 노드, 풀 멤버 | 복제, 정족수, 최소 활성 수 | 컨트롤러 설정, 노드 역할, 풀 설정 |

## 13. 표준이 서로 어긋나서 고른 것

| 갈림 | 고른 것 |
|---|---|
| 위치 계층: Redfish는 `Facility` 4단, NetBox는 중첩하는 위치 | 고정 타입 넷에 `RoomZone`, `Row`, `Aisle`을 더해요. 무한 중첩은 쓰지 않아요 |
| 전력 사슬의 범위: Redfish에 UPS, 발전기, 변압기, 인입이 없고 NetBox는 패널부터 | 둘을 합치고 상류는 프락시움이 정해요 |
| 이중화: Redfish는 장비 속성, NetBox는 급전선의 유형 표시 | 묶음 객체로 합쳐요. 속성과 표시는 입력 근거로만 써요 |
| 223P가 승인되지 않은 초안 | 개념(연결점, 매질)만 참고하고 어휘를 들이지 않아요 |
| 포트: RFC 8345는 종단 하나, OpenConfig는 인터페이스와 포트 컴포넌트 | `Interface` 하나와 `interface_kind` |
| LAG: RFC 8345는 직접 못 적고, OpenConfig는 `min-links` | `Lag`를 묶음으로 두고 `min_alive`가 `min-links`예요 |
| 마스킹 그룹: Swordfish가 `StorageGroup`을 deprecated로 바꿈 | `EXPORTED_TO` 링크와 번지지 않는 `InitiatorGroup` |
| "포함"이라는 말이 표준마다 다른 뜻 | `part_of`, `contained_in`, `member_of`, `record_only`로 갈라요 |
| 서비스 구분: CSDM은 계층이 풍부, Backstage는 소유·소속·의존을 소수 관계로 나눔 | 타입 이름은 CSDM, 링크는 Backstage처럼 나누고 만능 의존 링크를 쓰지 않아요 |

## 14. 고른 이름과 버린 이름

| 고른 이름 | 버린 이름 | 이유 |
|---|---|---|
| `PhysicalServer` | `Server`, `Host`, `Node`, `ComputerSystem` | `Server`는 역할과, `Node`는 쿠버네티스와 RFC 8345와, `Host`는 OpenTelemetry와 겹쳐요 |
| `VirtualMachine` | `Vm`, `Guest`, `Instance` | 약어와 제품 말을 피해요 |
| `ComputeCluster` | `Cluster` | `Cluster`만으로는 뜻이 일곱이에요 |
| `Interface` | `Port`, `TerminationPoint` | 하나로 모으고 종류는 `interface_kind`예요 |
| `Lag` | `PortChannel`, `Bond`, `Team` | 표준 용어이고 종류는 `lag_kind`예요 |
| `NetworkInstance` | `Vrf`, `RoutingInstance` | OpenConfig의 이름이고 L2까지 덮어요 |
| `RoutingSession` | `BgpSession`, `Neighbor` | 프로토콜 일반화예요 |
| `StorageVolume` | `Lun`, `Volume`, `Disk` | `Volume`은 컨테이너와 겹치고 LUN은 매핑 속성이에요 |
| `FailureDomain` | `AvailabilityZone`, `Zone` | 가용 영역은 `domain_kind`의 값이에요 |
| `Workload`, `ClusterService` | `Deployment`, `Service` | 종류는 `workload_kind`이고 `Service`는 서비스 계층과 겹쳐요 |
| `RedundancyGroup` | `RedundancySet`, `Group` | 번지지 않는 `LogicalGroup`과 구분해요 |
| 링크 `PART_OF`, `RUNS_ON` | `INSTALLED_IN`(초안의 이름), `HOSTS` | 부품은 `PART_OF`, 자리는 `LOCATED_IN`과 `MOUNTED_IN`, 방향은 출발에서 도착이에요 |

## 15. 넣지 않은 것

- 변경 요청, 사건, 절차서, 라이선스, 비용: E2E 프로세스를 구체화할 때 봐요. 무선과 생산 설비도 넣지 않아요.
- 사람의 개인 속성: 로그인 쪽의 불투명한 참조만 둬요.
- 수치의 시계열(온도, 전력, 사용률): KG에 쌓지 않고 가리키기만 해요.

## 16. 관련 결정과 요구사항

- [ADR-0007](adr/0007-kg-model.md): 이 문서의 결정. [KG의 모델](kg-model.md): 칸과 규칙. [KG에 묻기](kg-queries.md): 번짐의 계산.
- 요구사항: REQ-105(표준 모델), REQ-100(타입 등록부), REQ-101(링크의 뜻과 채널), REQ-102(이중화 묶음).
