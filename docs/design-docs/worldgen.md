---
title: 합성 세계 생성기 — 같은 시드이면 같은 데이터센터, 정답과 소스 보기와 변경 스트림과 분포 프로파일
kind: guide
status: active
covers:
  - backend/internal/worldgen/**
  - backend/cmd/praxium/worldgen.go
follows:
  - docs/design-docs/adr/0021-synthetic-world-generator.md
  - docs/design-docs/query-engine-shape.md
---
# 합성 세계 생성기

제품의 한 부분이에요(ADR-0021). 시드와 인자로 합성 데이터센터 한 세트를 **표준 모델의 타입과 링크만으로** 생성하고, 그 정답(진실)과, 같은 세계를 여러 가상 소스가 다르게 보여 주는 **소스 보기**와, 시간이 지나며 바뀌는 **변경 스트림**과, 세계의 **분포 프로파일**을 만들어요. 이후 단계가 이것으로 테스트해요(정제, 식별, 링크, 변경 배치, 영향 범위의 기준 구현과 규모).

이 문서의 숫자 가운데 측정한 것은 모두 **가정**이에요(머신 한 대, 한 번씩, 합성 데이터의 형태). 기본 인자도 이 생성기가 선택한 값이지 현실의 사실이 아니에요. 소스 이름, 제조사 이름, 컬럼 이름은 모두 가상의 것이고 실제 제품의 스키마와 무관해요.

## 1. 코드 위치

- 유닛 `core.worldgen`(`backend/internal/worldgen`)은 `core.kernel`과 `core.model`만 import해요. `os`와 `net`은 import하지 못해서 파일은 `Store`(파일을 생성하는 인터페이스)로 받고, 디스크는 명령이 제공해요.
- 명령은 `praxium worldgen`이에요(`cmd.praxium`의 하위 명령이고, 별도 실행 파일이 아니에요).

```
praxium worldgen [-seed N] [-scale N] [-out DIR] [-set 이름=값]... [-views] [-changes N] [-shape] [-shape-in 파일]
praxium worldgen validate truth.jsonl     # 정답 파일을 표준 모델로 검사
praxium worldgen params                   # 인자 이름 목록
```

`-out`이 없으면 아무것도 기록하지 않고 개수만 계산해요(내용 해시와 개수가 출력돼요). `-views`와 `-changes`는 `-out`이 있어야 해요.

## 2. 결정성

- 모든 무작위는 주입받은 `kernel.Rng`에서 생성돼요. 시계와 전역 난수는 사용하지 않아요. 세계는 `Build(등록부, 인자)`가 `NewSeededRng(Seed)`로 만들어요.
- 세계는 처음에 `Rng`에서 키 5개(객체, 구조, 서비스, 변경, 소스)를 생성하고, 이후 모든 값은 **id(주소)에서 계산**해요. 객체의 id 형식(`rack.3.1.4.7`, 부품은 `rack.3.1.4.7/srv12/mem3`)에 위치가 포함되고, 객체 하나의 속성 값은 (키, id)의 해시에서 나와요. 그래서 어떤 랙도 단독으로 다시 만들 수 있고 소스 보기와 변경 계획이 대상을 계산으로 찾아요. 객체를 저장하지 않아요.
- 같은 인자면 **같은 바이트**예요. 정답 파일과 모든 소스 파일과 매니페스트의 해시가 같아요(테스트로 고정해요).
- 규모를 맞추려고 생성기를 몇 번 예비 실행해(`calibrate`) 사이트 수와 랙 수를 정해요. 이것도 인자만의 함수예요.

## 3. 세계의 구조와 층별 범위

계층은 사이트(기본 랙 500개) → 홀(랙 24개) → 구역(열 2개) → 열(랙 8개) → 랙 → 서버(14대 ± 1)예요. 사이트는 지역에 속하고, 사이트끼리는 광역 케이블 링으로 연결돼요.

| 층 | 생성하는 것 | 이중화와 그룹 | 생성하지 않는 것(이유) |
|---|---|---|---|
| 시설 | 지역, 사이트, 건물, 홀, 구역, 열, 랙, 급전선, 변압기, 배전반, UPS(A와 B 면), 분전반, RPP, 랙 PDU, 냉각기 3대(N+1), 항온항습기, 냉각 루프 | 전력 A/B 두 면(`POWERED_BY`의 `feed_side`), 구역마다 항온항습기 N+1 또는 N+2(`cooling_n2_share`), 랙은 둘 이상의 장치에 `COOLED_BY` | 층, 통로, 버스웨이, 콘센트, 발전기(아직 사용처가 없어요) |
| 하드웨어 | 서버, BMC, CPU, 메모리, 드라이브, 전원 장치, 팬, 네트워크 어댑터 | 서버마다 전원 장치 2개의 2N 그룹(`PowerRedundancyGroup`), 팬 N+1 그룹 | 섀시와 블레이드, 센서 |
| 네트워크 | 스파인, 집선, ToR 스위치, 엣지 라우터 2대, 인터페이스, LAG, ECMP 그룹, 라우팅 인스턴스, HA 쌍 | ToR마다 상향 링크 2개(LAG, 최소 1), 집선 스위치마다 스파인 전부로의 ECMP 그룹, 라우터 HA 쌍, 서버는 ToR 2대에 연결 | IP 주소와 프리픽스, 라우팅 세션, MLAG, 방화벽, 가상 네트워크 |
| 스토리지 | 어레이(스토리지 랙마다), 컨트롤러 2개, 풀, RAID 그룹, 볼륨 | 컨트롤러 HA 쌍, RAID 패리티(드라이브 하나 허용) | 파일 시스템, 오브젝트 스토리지, SAN |
| 가상화 | 가상화 매니저, 가상 데이터센터, 클러스터, 호스트, 데이터스토어, VM, 가상 디스크, 가상 NIC | 클러스터의 용량 이중화 N+1 또는 N+2(`cluster_spare2_share`, `min_alive`가 호스트 수에서 뺀 값) | 리소스 풀, 배치 규칙 |
| 컨테이너 | 클러스터, 컨트롤 플레인, 네임스페이스, 워크로드, 노드, 파드, 컨테이너 | 컨트롤 플레인 쿼럼, 워크로드는 파드의 그룹 | 노드 풀, 서비스와 인그레스, 영구 볼륨 |
| 플랫폼 | 운영체제, 소프트웨어 인스턴스, 데이터베이스 클러스터와 인스턴스, LB 가상 서버와 풀과 풀 멤버 | DB 2대(active_standby), LB 풀은 멤버 n 가운데 k | 인증서, DNS, 엔드포인트 |
| 서비스 | 업무 서비스, 기술 서비스, 응용 서비스, 팀, 환경 | 응용 서비스마다 2~4개 멤버 가운데 k(`strict_service_share`는 모두 필요) | 서비스 제공물, 계약 |

- **서비스 의존.** `CALLS`(응용에서 응용, 호출)와 `DEPENDS_ON`(응용에서 응용 서비스와 데이터베이스 클러스터와 엔드포인트로의 연쇄 의존), `REALIZED_BY`, `OWNED_BY`로 구성한 희소 그래프예요. 호출하는 서비스는 자신보다 한 층 이상 깊은 서비스(`service_depth`)만 호출해서 비순환이고, `cycle_ratio` 비율만큼은 역방향 호출을 추가해 **두 서비스가 서로 호출하는 순환**이 생겨요. `cross_site_ratio` 비율만큼은 다른 사이트의 서비스를 호출해요. 응용 서비스마다 첫 호출 대상에는 `DEPENDS_ON`도 추가해요.
- **허브.** 환경, 팀, 가상화 매니저는 연결이 수천 개인 허브예요(실제 전역 서비스는 생성하지 않아요).
- **모든 타입에 키가 있다(TD-0009 해소, ADR-0022).** 생성하는 모든 객체는 키를 만들 수 있어서 `not_ingestible`은 정답에 나오지 않아요. 키를 만들 수 없는 객체를 `key_not_formed`로, 키가 없는 타입을 `no_identity_key`로 표시하는 경로는 확장 타입을 위해 남아 있고 테스트가 있어요. 집합 키(`MlagDomain`)는 프리셋이 `members(링크)`로 "이 링크를 생성한다"고 보장해야 만들 수 있다고 봐요.
- **이중화 그룹은 객체예요(ADR-0022).** 구성원은 `MEMBER_OF`로 그룹에 포함되고, 사용하는 쪽은 **그룹에** 의존해요(구성원 하나에 직접 연결하지 않아요). 검증기가 "구성원 수 ≥ `min_alive`", "그룹에 의존하는 객체가 하나 이상", "피드 링크(`POWERED_BY`, `COOLED_BY`, `UPLINKED_VIA`)가 구성원 하나로 연결되지 않음"을 검증해요.

| 영역 | 그룹 타입 | 구성원 | 사용하는 쪽과 링크(채널) |
|---|---|---|---|
| 냉각 | `CoolingRedundancyGroup`(구역마다 N+1 또는 N+2, 사이트의 냉동기 그룹, 서버의 팬 그룹) | 항온항습기, 냉동기, 팬 | 랙 `COOLED_BY`(`cooling`), 냉각 루프 `COOLED_BY`, 서버 `COOLED_BY` |
| 전원 | `PowerRedundancyGroup`(사이트의 면마다 UPS 그룹, 서버마다 급전 장치 2N) | UPS, 급전 장치 | 전력반 `POWERED_BY`(`power`), 서버 `POWERED_BY` |
| 네트워크 | `MlagDomain`(랙마다 ToR 2대), `Lag`(ToR 상향), `EcmpGroup`(집선 상향), `HaPair`(엣지 라우터) | 스위치, 인터페이스, 라우터 | 서버, ToR, 집선, 스파인의 `UPLINKED_VIA`(`network`) |
| 스토리지 | `HaPair`(어레이 컨트롤러 2개), `RaidGroup` | 컨트롤러, 드라이브 | 볼륨 `RUNS_ON`(`service`), 풀 `BACKED_BY`(`storage`) |
| 클러스터 용량 | `ComputeCluster`(N+1, N+2) | 호스트 | 기술 서비스 `REALIZED_BY` |
| 서비스 | `LbPool`(k-of-n), `DatabaseCluster`, `Workload`, `ControlPlane` | 풀 멤버, DB 인스턴스, 파드, 노드 | 응용 서비스 `REALIZED_BY`와 `DEPENDS_ON`(`service`), 워크로드 `RUNS_ON` 컨트롤 플레인 |

## 4. 인자

`-set 이름=값`으로 모두 바꿀 수 있어요(`praxium worldgen params`). 0은 "사용 안 함"이 아니라 그 값 그대로이고, 범위를 벗어나면 `invalid_value`로 거부해요(`Validate`). 매니페스트에는 **실제로 사용한 값**이 들어가요.

| 분류 | 인자(기본) | 설명 |
|---|---|---|
| 규모 | `seed`(1), `scale`(100000), `sites`(0=규모에서 결정) | 객체와 링크를 합쳐 대략 `scale`개 |
| 하위 개수(fan-out) | `racks_per_hall`(24), `racks_per_row`(8), `rows_per_zone`(2), `servers_per_rack`(14), `storage_rack_every`(8), `racks_per_cluster`(3), `agg_per_plane`(2), `spines_per_site`(4), `ups_per_side`(1), `cooling_units_n`(3), `sites_per_region`(4) | 층마다 하위 요소가 몇 개인가 |
| 서버 내부 | `memory_modules`(4), `drives`(2), `fans`(3) | |
| 가상화와 플랫폼 | `host_share`(0.7), `vms_per_host`(8), `node_host_share`(0.25), `pods_per_node`(6), `workloads_per_cluster`(6) | |
| 서비스 | `service_density`(VM당 0.04), `service_depth`(4), `dependency_degree`(1.6), `cross_site_ratio`(0.03), `cycle_ratio`(0.02), `database_share`(0.3) | |
| 이중화 구성 비율 | `cooling_n2_share`(0.3), `cluster_spare2_share`(0.3), `strict_service_share`(0.3) | |
| 근거 | `unknown_ratio`(0.03), `assumed_ratio`(0.02), `fill_rate`(0.35) | 값을 모르는 속성의 비율, 가정으로 채운 값의 비율, 선택 속성을 채우는 비율 |
| 결함 | `missing_row_ratio`(0.01), `duplicate_key_ratio`(0.005), `false_completeness`(true), `shape_drift`(true), `unparseable_ratio`(0.002), `late_event_ratio`(0.05), `duplicate_event_ratio`(0.03), `ecmp_mismatch`(true) | 5절 |
| 변경 | `change_events`(0), `vm_move_share` 외 6개 비율, `flaps_per_object`(4) | 7절 |

규모의 오차는 큰 세계(10만 개 넘게)에서 3% 안이고 아주 작은 세계(몇천 개)에서는 랙 하나가 몇천 개라 30%까지 벗어나요.

## 5. 정답 형식(v1)

`truth.jsonl`은 라인 하나에 JSON 하나예요. 첫 라인은 헤더(`{"kind":"header","format":"praxium-worldgen-truth","version":1}`)이고, 나머지는 객체와 링크가 생성된 순서대로예요(부모가 먼저, 서비스 호출은 나중 사이트를 가리킬 수 있어서 끝점 검사는 파일 전체를 읽은 뒤에 해요).

| 구분 | 필드 | 설명 |
|---|---|---|
| 객체 | `kind:"object"`, `id`, `type`(예 `ot.rack`), `props`, `unknown`, `assumed`, `identifiers`, `grade`, `basis`, `not_ingestible` | `props`는 속성 이름 → 값, `unknown`은 세계가 값을 모르는 속성(`props`에 없음), `assumed`는 가정으로 채운 속성, `identifiers`는 식별자 방식(`is.*`) → 값. 비어 있는 필드는 기록하지 않아요 |
| 링크 | `kind:"link"`, `id`, `type`(예 `lt.powered_by`), `from`, `to`, `props`, `grade`, `basis`, `not_ingestible` | 끝점은 객체 id. 링크 id는 (타입, 시작, 끝)의 96비트 해시 |

- `grade`는 `fact`와 `assumed`(링크는 `assumed_ratio`로), `basis`는 층에서 정해져요(시설과 서비스는 `declared`, 하드웨어와 가상화는 `observed`, 네트워크와 스토리지는 `configured`, `CABLED_TO`는 `observed`).
- 값은 값 타입의 표준 형식이에요(MAC은 소문자 12자리, 도메인 이름은 소문자). `Validate`가 이것을 등록부로 검증해요.
- `Validate`는 알려진 타입과 속성, 필수 속성(모르는 값이 아니면), 열거 값, 정수와 불리언, 값 타입의 표준 형식, 링크 바인딩, 끝점 존재, id의 유일성, **식별 키의 유일성**(속성, 문맥, 식별자 방식), 그룹의 `1 ≤ min_alive ≤ 멤버 수`를 검사해요. 객체마다 키 문자열을 보유하고 있어서 메모리에 올라가는 크기의 세계용이에요(큰 세계는 개수 계산과 해시만 하고 이 검사는 하지 않아요).

## 6. 매니페스트와 정답 이외의 파일

`manifest.json`(`format: praxium-worldgen-manifest`, `version: 1`)의 필드: `model_version`과 `model_hash`, `params`(사용한 값), `layout`(사이트, 홀, 랙, 서버, 호스트, VM, 서비스 수), `counts`(객체와 링크의 합, 수집 불가의 수, **타입별과 링크 타입별과 층별**), `truth`(파일, 라인 수, 바이트, SHA-256), `content_hash`(정답의 SHA-256, `-out` 없이도 출력돼요), `files`(모든 출력 파일의 라인 수와 바이트와 SHA-256), `sources`(소스마다 타입과 속성), `imperfections`, `change_events`, `shape`.

## 7. 소스 보기

같은 세계를 가상 소스 6개가 서로 다르게 보여 줘요(`-views`). 소스마다 **자기 식별자**를 가져서 같은 객체가 다른 이름으로 나와요(식별자는 소스 이름과 정답 id의 해시라 어느 쪽에서도 계산돼요).

| 소스 | 형식 | 파일 | 포함 내용 |
|---|---|---|---|
| `asset-register` | CSV | `racks.csv`, `servers.csv`, `switches.csv` | 랙, 서버, 스위치와 라우터. `asset_id`, 이름, 일련번호, 제조사, 랙의 `asset_id`(부모 참조) |
| `virt-inventory` | JSON API 페이지(100건씩, `next`) | `virt/hosts-NNNN.json`, `virt/vms-NNNN.json` | 호스트와 VM. 안에 게스트(`guest`), NIC와 디스크의 **배열**과 중첩 객체. **배치 측** 속성: 이름, vCPU, 메모리, 게스트 |
| `netconf` | 들여쓰기 형식의 장치 설정 텍스트 | `netconf/config-site-N.cfg` | 스위치와 라우터: `interface`, `lag`, `ecmp` 그룹과 `member` 라인 |
| `show-tables` | 고정 폭 명령 출력 테이블 | `show/ecmp-site-N.txt` | ECMP 그룹(장치 0~35, 그룹 36~71, 경로 수 72~77, 멤버는 78부터) |
| `declared-lists` | JSON Lines | `declared/ecmp-site-N.jsonl` | 선언된 ECMP 그룹과 멤버 |
| `change-feed` | JSON Lines | `changes/events.jsonl` | VM의 `power_state` 같은 **스트림 측** 속성, 구성의 추가와 변경과 삭제 |

- **소스 간 중복.** 서버는 `asset-register`(`asset_id`와 일련번호)와 `virt-inventory`(`host_id`와 하드웨어 일련번호)와 `netconf`/`show-tables`(호스트 이름)에 나와 **일련번호가 공통 키**예요. VM의 `power_state`는 배치 측이 제공하지 않고 스트림이 제공해요(같은 타입의 속성이 두 소스로 나뉘어요). **같은 ECMP 그룹은 세 가지 경로로 도출**돼요(`netconf`의 `ecmp` 블록, `show-tables`의 라인, `declared-lists`의 라인).
- 행과 필드가 비어 있으면 정답에서 그 속성이 `unknown`이거나 채워지지 않았다는 의미예요.

### 소스 보기의 결함

인자로 켜고 꺼요. 결함을 만들 때마다 매니페스트의 `imperfections`에 **종류별 합계와 파일별 합계**가 남고, 종류마다 처음 25개는 `samples`에 파일과 대상과 **기대 결과**와 함께 남아요.

| 종류 | 대상 | 기대 결과 |
|---|---|---|
| `missing_row` | CSV 행, JSON 항목 | 그 소스에서만 없음. 삭제로 보지 않고 다른 소스와 병합해 존재로 처리 |
| `duplicate_key` | CSV 행, JSON 항목 | 같은 키의 두 행이 한 객체. 속성은 뒤의 행이 우선 |
| `false_completeness` | `virt-inventory`의 마지막 페이지 | `complete:true`와 누락된 행까지 센 `total`을 주장. 완전하다는 주장을 신뢰하지 않고 삭제를 추정하지 않음. 이 인자가 꺼지면 소스는 행을 잃을 때 `complete:false`로 정확히 알림 |
| `shape_drift` | `servers.csv`의 `serial_no`(원래 `serial_number`), `vms-0002.json`부터 `hypervisor`(원래 `host`) | 컬럼 이름의 별칭으로 매칭하거나 컬럼을 격리. 일련번호를 빈 값으로 읽지 않음 |
| `unparseable_line` | `netconf`의 장치 블록, `events.jsonl` | 해당 라인을 거부하고 개수를 세며, 나머지는 읽음 |
| `late_event`와 `duplicate_event` | 변경 스트림 | 늦게 도착한 이벤트는 `source_time`이 더 오래된 값이라 무시(지연 도착 값 처리), 같은 `event_id`는 한 번만 |
| `ecmp_mismatch` | `show-tables`의 첫 ECMP 그룹 | 테이블이 멤버를 하나 덜 보고함. 설정과 선언된 목록이 같으니 이 둘로 도출하고 불일치를 표시 |

## 8. 변경 스트림

`-changes N`은 변경 N개의 계획을 수립해요(`Plan`). 대상은 계산으로 찾고, 새로 생기는 객체는 같은 생성기로 만들어요.

| 종류 | 효과 |
|---|---|
| `vm_move` | VM의 `RUNS_ON` 링크를 다른 호스트로(옛 링크 삭제, 새 링크 추가) |
| `replace` | 메모리 모듈 하나 삭제, 새 모듈과 `PART_OF` 추가 |
| `link_change` | 서버의 `UPLINKED_VIA` 링크의 `network_role` 변경 |
| `retire` | VM과 그 게스트와 NIC와 디스크 삭제(노드를 보유한 VM은 제외) |
| `flap` | VM의 `power_state`가 `flaps_per_object`회 번갈아 바뀜(변경 속도가 `dynamic`) |
| `add` | 호스트에 새 VM 추가 |

- 이벤트 라인: `event_id`, `source`, `source_time`(기준 시각 2026-01-01에서 계획 순서대로 60초씩), `op`(`added`/`modified`/`deleted`), `kind`, `type`, `id`(**소스의 id**), 링크는 `from`과 `to`, `props`, `identifiers`.
- 전달 순서는 시간순에서 `late_event_ratio` 비율만큼 최대 30건 뒤로 밀려요. `duplicate_event_ratio` 비율만큼은 같은 라인이 두 번 나오고, `unparseable_ratio` 비율만큼은 잘린 라인이 섞여요.
- **기대 상태.** `changes/expected.jsonl`에 계획이 건드린 객체와 링크마다 최종 상태(`present`/`absent`)와 바뀐 속성이 정답의 id로 있어요. 올바른 처리 방식(깨진 라인은 개수를 세고 건너뛰기, 이미 처리한 `event_id`는 무시, 객체에 마지막으로 적용된 값보다 오래된 `source_time`은 무시)으로 스트림을 재생하면(`ReplayChanges`) 이 상태가 돼요(`CheckReplay`). 테스트로 고정해요.
- 한계: 연쇄 삭제는 계획이 명시한 것만 있어요(VM을 없애도 그 위 소프트웨어 인스턴스는 남고 `RUNS_ON` 링크만 끊겨요).

## 9. 분포 프로파일

`shape-card.json`(`praxium-worldgen-shape`, v1, `-shape`)은 query-engine-shape.md 6절이 언급한 값을 제공해요.

| 필드 | 설명 |
|---|---|
| `out_degree`, `in_degree` | 연결 수의 분포: 개수, 평균, 최대, p50/p90/p99, 2의 거듭제곱 구간의 `buckets` |
| `containment_depth` | `LOCATED_IN`과 `PART_OF`와 `MOUNTED_IN`을 따라간 깊이의 분포 |
| `service_depth`, `service_calls_per_service`, `service_cycle_pairs`, `services_unreached_from_roots` | 사이트 안 호출 그래프에서 루트(아무도 호출하지 않는 서비스)로부터의 거리, 서비스당 호출 수, 서로 호출하는 쌍, 루트에서 도달하지 못하는 서비스 |
| `groups` | 그룹 타입마다 그룹 수, 멤버 수 분포, 여유(`멤버 수 - min_alive`) 분포, `mode`별 개수 |
| `cross_site` | 링크와 호출의 사이트 간 교차 수와 비율(`LOCATED_IN`은 지역 때문에 제외해요) |

사이트 하나씩 집계하므로 메모리는 **가장 큰 사이트**의 크기만큼이에요(객체당 약 150바이트로 가정).

**입력으로 사용하기.** `-shape-in 파일`은 `cross_site.call_ratio`를 `cross_site_ratio`로, `service_depth.max + 1`을 `service_depth`로, `service_calls_per_service`를 `dependency_degree`로 설정해요(`-set`이 그 위에 덮어써요). **지원하지 않는 것**은 실행할 때마다 출력해요: 연결 수의 분포(인자가 없고 하위 개수(fan-out)에서 결정돼요), 포함의 깊이(표준 모델의 계층으로 정해져요), 그룹의 여유(프리셋이 선택한 값), 허브(생성하지 않아요).

## 10. 규모와 측정

데이터는 관찰자(`Observer`)로 스트리밍해요. 생성기는 객체와 링크를 만드는 즉시 관찰자들(정답 기록, 소스 보기, 분포 프로파일)에게 전달하고 아무것도 누적하지 않아요. 메모리는 **세계의 크기와 상관없이 랙 하나, 홀 하나, VM 하나, 사이트 하나(분포 프로파일과 장치 설정)** 만큼이에요. 변경 계획만 이벤트 수에 비례해 메모리에 유지돼요.

측정(가정): 2코어, 7GB, 디스크 여유 21GB의 머신 한 대에서, 정답을 직렬화하고 SHA-256까지 계산하되 **파일에 기록하지 않고**(`-out` 없이) 집계한 값이에요.

| 규모(객체+링크) | 사이트 | 시간 | 처리량 | 최대 상주 메모리 |
|---|---|---|---|---|
| 약 98만 | 2 | 0.9초 | 약 110만 건/초 | 15MB |
| 약 976만 | 11 | 7.7초 | 약 127만 건/초 | 15MB |
| 약 9777만 | 104 | 78.6초 | 약 124만 건/초 | 15MB |

- 정답을 디스크에 기록하면 약 98만 건이 243MB예요(건당 약 250바이트). 1억 건은 약 25GB라 이 머신에는 기록하지 않고 개수만 셌어요.
- 소스 보기, 변경 1만 건, 분포 프로파일을 모두 켠 98만 건: 1.5초, 최대 113MB. 분포 프로파일만 켠 976만 건: 11초, 최대 87MB.
- 테스트(`memory_test.go`)가 10만 건과 100만 건의 힙이 비슷하고(약 6MB) 100만 건에서도 64MB 아래인 것을 고정해요.

## 11. 이후 단계에서 사용하는 방법

- **수집과 정제**: 소스 보기 파일이 입력이에요. 정답과 비교해서 "무엇을 복원했고, 무엇을 결함으로 감지하는가"를 측정해요. 매니페스트의 `imperfections`가 기대 결과예요.
- **식별과 병합**: 소스 간 중복(일련번호가 공통)과 정답의 `identifiers`가 기준 정보예요.
- **변경 배치와 지연 도착 값 처리**: `events.jsonl`과 `expected.jsonl`.
- **영향 범위의 기준 구현**: 정답의 링크와 그룹의 `min_alive`와 서비스 그래프에서 기대 응답을 만들어요. 분포 프로파일은 실제 소스를 연결했을 때 같은 필드로 다시 조정하는 방법이에요.
- **규모**: `-scale`로 100만, 1000만, 1억까지 `-out` 없이 스트리밍하고, 엔진에 직접 입력하려면 `Observer`를 구현하면 돼요(정답 파일 없이).

## 12. 채택한 해석

문서에 명시가 없거나 문서끼리 어긋나는 곳에서 가장 단순한 해석을 채택했어요.

1. 규모의 기준은 객체와 링크의 합이에요.
2. 링크의 방향은 표준 모델의 바인딩 그대로예요(`MEMBER_OF`는 멤버에서 그룹, `PART_OF`는 부품에서 전체). 랙에 장착하는 장비는 `LOCATED_IN`이 아니라 `MOUNTED_IN`만 사용해요(바인딩이 그래요).
3. 응용 서비스끼리의 연쇄 의존은 `DEPENDS_ON`(ADR-0022에서 바인딩을 추가), 호출은 `CALLS`예요. k-of-n 멤버 그룹은 `LbPool`이고 응용 서비스에 `PART_OF`로 연결해요(ADR-0022).
4. `evidence grade`는 `fact`가 기본이고 일부가 `assumed`예요. `unknown`은 값이 없는 속성의 목록으로 나타내요.
5. 키가 없는 타입은 이제 없어요(ADR-0022). 키를 만들 수 없는 객체는 수집 불가로 표시하고 건너뛰지 않아요.
6. 식별자의 폭: MAC은 46비트(무작위인 부분), 일련번호는 60비트, 소스 식별자는 50비트예요. 아주 큰 세계(1억 건)에서 인터페이스 MAC(약한 키)은 드물게 중복될 수 있어요(생일 문제로 몇 쌍). 강한 키인 BMC의 MAC과 일련번호는 중복될 확률이 무시할 수 있을 만큼 작아요. 검증은 메모리에 올라가는 세계에서만 해요.
7. 매니페스트의 `samples`는 종류당 25개로 제한해요(수가 많은 세계에서 매니페스트가 커지지 않게). 합계는 모두 정확해요.
8. 변경 스트림의 `id`는 정답의 id가 아니라 소스의 id예요(소스는 자기 이름 기준으로 보고해요). 기대 상태만 정답의 id를 사용해요.

## 13. 알려진 한계와 설계 담당에게 확인할 사항

- 모든 수는 합성 데이터의 형태이고 기본 인자는 이 생성기가 선택한 값이에요. 실제 환경의 형태와 다를 수 있어서 실제 소스를 연결한 뒤 분포 프로파일로 다시 조정해요.
- 분포 프로파일은 한 사이트가 메모리에 올라가는 크기(약 150바이트 × 객체 수, 분포 프로파일을 켤 때)까지 지원해요. 사이트가 하나인 1억 건 세계는 `-shape` 없이 스트리밍해요.
- 규모의 오차: 큰 세계 3%, 몇천 건의 세계 30%.
- **질문 1~3은 해결됐어요**(ADR-0022): 시설 그룹은 객체로 정의하고, `EcmpGroup`과 `MlagDomain`의 키는 자기 것이고, `DEPENDS_ON`은 응용 서비스 사이에도 연결해요.
- **질문 4**: `UpsPerSide`가 1이면 UPS 그룹은 구성원이 하나라 `min_alive`가 1이에요(`load_shared`). 이것도 그룹으로 둬도 되나요.
