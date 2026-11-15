---
title: 게이트웨이의 액션 타입 표 — 바꾸는 액션 95개의 종류, 효과, 관측 시점, 1차 대상
kind: guide
status: active
follows:
  - docs/design-docs/adr/0061-gateway-as-a-praxium-component-approval-first.md
  - backend/internal/model/standard/actions.json
---
# 게이트웨이의 액션 타입 표

표준 모델의 액션 타입 112개를 읽고, 게이트웨이가 처음 열 때(1차) 어느 것을 대상으로 삼을 수 있는지 가렸어요. 구조와 책임은 [게이트웨이 설계](gateway.md), 안전과 여는 조건은 [게이트웨이의 안전](gateway-safety.md)에 있어요(ADR-0061). 이 표는 표준 모델 파일을 고치지 않고, 파일의 `reversibility`, `disruption`, `physical`, `mutates`와 효과 문장(`declared_effects`)만 읽어서 만들었어요.

## 1. 판단 규칙

1차(승인 전용)의 대상은 네 조건을 모두 만족하는 액션이에요.

1. 되돌릴 수 있어요(`reversibility`가 `reversible`).
2. 지장을 주지 않아요(`disruption`이 `none`).
3. 객체 하나에만 작용하고, 효과가 속성 하나예요. 객체를 만들거나 없애는 것, 링크를 바꾸는 것, 여러 효과가 섞인 것은 아니에요.
4. 효과가 곧바로 관측돼요. 값을 쓰면 다음 읽기에서 바로 보이는 것이에요.

물리 작업(`physical`)은 사람이 현장에서 하므로 1차에서 뺐어요. 읽는 쪽 소스가 같은 값을 다시 읽을 수 있어야 하는 것은 [게이트웨이 설계](gateway.md)의 확인 절이 정해요.

관측 시점은 효과 문장만 보고 세 가지로 갈랐어요. 문장에 근거가 없으면 추측하지 않고 "판단 못 함"으로 셌어요.

| 관측 시점 | 의미 | 근거 낱말 |
|---|---|---|
| 곧바로 | 쓴 값이 다음 읽기에 보여요 | 설정값, 표시 |
| 안정된 뒤 | 상태가 바뀌는 데 시간이 걸려요 | 상태 전환, 재시작·단절 |
| 다음 동기화 | 대상 시스템이 아니라 모델의 링크나 객체가 바뀌거나, 현장 작업이라 다음 수집이 돼야 알아요 | 링크·객체, 현장 작업 |
| 판단 못 함 | 효과 문장이 없거나 모호해요 | 글 없음 |

종류 열은 `가역`/`불가역`, `무지장`/`지장`(서비스에 영향 또는 데이터 손실), 물리 작업이면 `물리`를 이어 적었어요.

## 2. 집계

| 거르는 단계 | 수 |
|---|---|
| 액션 타입 전체 | 112 |
| 읽기 액션(바꾸지 않음) | 17 |
| 바꾸는 액션 | 95 |
| 그중 되돌릴 수 있음 | 71 |
| 그중 지장 없음(물리 포함) | 43 |
| 그중 물리 작업이 아님 | 37 |
| 효과 형태과 관측 시점까지 통과 | 12 |
| **1차 대상** | **10** |
| 경계(설계 검토 전까지 뺌) | 2 |

바꾸는 액션 95개의 효과 형태은 속성 34, 링크 13, 객체 15, 복합 22, 판단 못 함 11이에요. 관측 시점은 곧바로 14, 안정된 뒤 33, 다음 동기화 43, 판단 못 함 5예요. 모양과 시점이 둘 다 판단 못 함인 것이 5개이고, 둘 중 하나라도 판단 못 함인 것은 11개예요. 이런 액션은 인자의 정의(4절)가 생길 때까지 1차에 넣지 않아요.

읽기 17개 가운데 4개(`run_diagnostics`, `rescan_storage`, `test_connectivity`, `run_health_check`)는 부작용이 있을 수 있는 진단이에요. 이 구분은 이름과 모델의 표시로 한 것이라, 인자의 정의가 생기면 다시 가려요.

## 3. 1차 대상과 경계

1차 대상 10개예요: `set_cooling_setpoint`, `set_locator_led`, `clear_power_cap`, `set_fan_mode`, `cordon_node`, `uncordon_node`, `cancel_maintenance`, `exit_maintenance_mode`, `acknowledge_alarm`, `set_label`.

경계 2개는 1차에서 뺐고, 설계 검토에서 정해요.

- `set_power_cap`: 모델의 표시는 지장 없음인데 효과 문장에 "성능 저하 가능"이 있어서 표시와 문장이 어긋나요.
- `set_maintenance_mode`: 대상 밖의 알림을 억제하는 효과가 있어서 "객체 하나, 속성 하나"가 아니에요.

켜는 계열(`power_on`, `turn_on_outlet`, `enable_interface`, `start_*`, `enable_pool_member`)은 효과가 상태 전환이라 곧바로 관측되지 않아서 뺐어요. 링크나 객체를 만드는 액션은 조건 3에서 빠져요.

## 4. 할 일

- 액션 타입에 인자의 정의(이름, 종류, 허용값)를 더해요. 표준 모델을 고치는 일이라 이 문서의 범위 밖이에요.
- 효과 문장 대신 기계가 읽는 효과 선언(어느 속성이 어떤 값이 되는가, 관측 시점)을 더해요. 그러면 표의 "판단 못 함"이 줄어요.
- 표는 효과 문장을 사람이 읽고 만든 거예요. 선언이 생기면 검사로 대체해요.

## 5. 바꾸는 액션 95개

| 액션 | 종류 | 효과 | 관측 시점 | 근거 | 1차 |
|---|---|---|---|---|---|
| `set_cooling_setpoint` | 가역·무지장 | 속성 | 곧바로 | 설정값 | 예 |
| `start_cooling_unit` | 가역·무지장 | 복합 | 안정된 뒤 | 상태 전환 | - |
| `stop_cooling_unit` | 가역·지장 | 복합 | 안정된 뒤 | 상태 전환 | - |
| `replace_filter` | 불가역·무지장·물리 | 속성 | 다음 동기화 | 현장 작업 | - |
| `transfer_power_source` | 가역·지장 | 속성 | 안정된 뒤 | 재시작·단절 | - |
| `start_generator` | 가역·무지장 | 속성 | 안정된 뒤 | 상태 전환 | - |
| `stop_generator` | 가역·지장 | 복합 | 안정된 뒤 | 상태 전환 | - |
| `run_ups_self_test` | 가역·무지장 | 복합 | 안정된 뒤 | 재시작·단절 | - |
| `bypass_ups` | 가역·지장 | 속성 | 안정된 뒤 | 상태 전환 | - |
| `exit_ups_bypass` | 가역·무지장 | 속성 | 안정된 뒤 | 상태 전환 | - |
| `open_breaker` | 가역·무지장·물리 | 복합 | 다음 동기화 | 현장 작업 | - |
| `close_breaker` | 가역·무지장·물리 | 속성 | 다음 동기화 | 현장 작업 | - |
| `turn_on_outlet` | 가역·무지장 | 속성 | 안정된 뒤 | 상태 전환 | - |
| `turn_off_outlet` | 가역·지장 | 복합 | 안정된 뒤 | 상태 전환 | - |
| `cycle_outlet` | 불가역·지장 | 속성 | 안정된 뒤 | 재시작·단절 | - |
| `install_device` | 가역·무지장·물리 | 복합 | 다음 동기화 | 현장 작업 | - |
| `remove_device` | 가역·무지장·물리 | 복합 | 다음 동기화 | 현장 작업 | - |
| `decommission_device` | 불가역·무지장·물리 | 복합 | 다음 동기화 | 현장 작업 | - |
| `replace_component` | 불가역·무지장·물리 | 복합 | 다음 동기화 | 현장 작업 | - |
| `reseat_component` | 불가역·무지장·물리 | 속성 | 다음 동기화 | 현장 작업 | - |
| `connect_cable` | 가역·무지장·물리 | 링크 | 다음 동기화 | 현장 작업 | - |
| `disconnect_cable` | 가역·무지장·물리 | 링크 | 다음 동기화 | 현장 작업 | - |
| `update_firmware` | 가역·지장 | 속성 | 안정된 뒤 | 재시작·단절 | - |
| `rollback_firmware` | 가역·지장 | 속성 | 안정된 뒤 | 재시작·단절 | - |
| `set_locator_led` | 가역·무지장 | 속성 | 곧바로 | 표시 | 예 |
| `set_power_cap` | 가역·무지장 | 속성 | 곧바로 | 설정값 | 경계 |
| `clear_power_cap` | 가역·무지장 | 속성 | 곧바로 | 설정값 | 예 |
| `set_fan_mode` | 가역·무지장 | 속성 | 곧바로 | 설정값 | 예 |
| `disable_interface` | 가역·지장 | 복합 | 다음 동기화 | 링크·객체 | - |
| `enable_interface` | 가역·무지장 | 속성 | 안정된 뒤 | 상태 전환 | - |
| `set_lag_min_links` | 가역·지장 | 속성 | 곧바로 | 설정값 | - |
| `add_lag_member` | 가역·무지장 | 링크 | 다음 동기화 | 링크·객체 | - |
| `remove_lag_member` | 가역·지장 | 링크 | 다음 동기화 | 링크·객체 | - |
| `add_vlan_to_interface` | 가역·무지장 | 링크 | 다음 동기화 | 링크·객체 | - |
| `remove_vlan_from_interface` | 가역·지장 | 링크 | 다음 동기화 | 링크·객체 | - |
| `clear_routing_session` | 불가역·지장 | 속성 | 안정된 뒤 | 재시작·단절 | - |
| `isolate_device` | 가역·지장 | 판단 못 함 | 안정된 뒤 | 재시작·단절 | - |
| `rejoin_device` | 가역·무지장 | 판단 못 함 | 판단 못 함 | 글 없음 | - |
| `failover_ha_pair` | 가역·지장 | 링크 | 다음 동기화 | 링크·객체 | - |
| `failback_ha_pair` | 가역·지장 | 링크 | 다음 동기화 | 링크·객체 | - |
| `create_volume` | 가역·무지장 | 객체 | 다음 동기화 | 링크·객체 | - |
| `delete_volume` | 불가역·손실 | 객체 | 다음 동기화 | 링크·객체 | - |
| `extend_volume` | 불가역·무지장 | 속성 | 곧바로 | 설정값 | - |
| `map_volume` | 가역·무지장 | 링크 | 다음 동기화 | 링크·객체 | - |
| `unmap_volume` | 가역·지장 | 링크 | 다음 동기화 | 링크·객체 | - |
| `create_snapshot` | 가역·무지장 | 객체 | 다음 동기화 | 링크·객체 | - |
| `delete_snapshot` | 불가역·손실 | 객체 | 다음 동기화 | 링크·객체 | - |
| `restore_snapshot` | 불가역·손실 | 판단 못 함 | 안정된 뒤 | 재시작·단절 | - |
| `fail_over_replication` | 가역·지장 | 판단 못 함 | 판단 못 함 | 글 없음 | - |
| `fail_back_replication` | 가역·지장 | 판단 못 함 | 판단 못 함 | 글 없음 | - |
| `create_virtual_machine` | 가역·무지장 | 객체 | 다음 동기화 | 링크·객체 | - |
| `delete_virtual_machine` | 불가역·손실 | 객체 | 다음 동기화 | 링크·객체 | - |
| `clone_virtual_machine` | 가역·무지장 | 객체 | 다음 동기화 | 링크·객체 | - |
| `migrate_virtual_machine` | 가역·무지장 | 링크 | 다음 동기화 | 링크·객체 | - |
| `resize_virtual_machine` | 가역·지장 | 속성 | 안정된 뒤 | 재시작·단절 | - |
| `create_vm_snapshot` | 가역·무지장 | 객체 | 다음 동기화 | 링크·객체 | - |
| `delete_vm_snapshot` | 불가역·손실 | 객체 | 다음 동기화 | 링크·객체 | - |
| `revert_vm_snapshot` | 불가역·손실 | 판단 못 함 | 안정된 뒤 | 재시작·단절 | - |
| `attach_virtual_disk` | 가역·무지장 | 객체 | 다음 동기화 | 링크·객체 | - |
| `detach_virtual_disk` | 가역·지장 | 객체 | 다음 동기화 | 링크·객체 | - |
| `evacuate_host` | 가역·지장 | 복합 | 다음 동기화 | 링크·객체 | - |
| `scale_workload` | 가역·무지장 | 복합 | 다음 동기화 | 링크·객체 | - |
| `restart_workload` | 불가역·지장 | 복합 | 안정된 뒤 | 재시작·단절 | - |
| `rollback_workload` | 불가역·지장 | 판단 못 함 | 판단 못 함 | 글 없음 | - |
| `delete_pod` | 불가역·지장 | 객체 | 다음 동기화 | 링크·객체 | - |
| `evict_pod` | 불가역·지장 | 링크 | 다음 동기화 | 링크·객체 | - |
| `cordon_node` | 가역·무지장 | 속성 | 곧바로 | 설정값 | 예 |
| `uncordon_node` | 가역·무지장 | 속성 | 곧바로 | 설정값 | 예 |
| `drain_node` | 가역·지장 | 복합 | 다음 동기화 | 링크·객체 | - |
| `start_service` | 가역·무지장 | 속성 | 안정된 뒤 | 상태 전환 | - |
| `stop_service` | 가역·지장 | 복합 | 안정된 뒤 | 상태 전환 | - |
| `restart_service` | 불가역·지장 | 속성 | 안정된 뒤 | 재시작·단절 | - |
| `renew_certificate` | 불가역·지장 | 복합 | 다음 동기화 | 링크·객체 | - |
| `add_dns_record` | 가역·무지장 | 객체 | 다음 동기화 | 링크·객체 | - |
| `remove_dns_record` | 가역·지장 | 객체 | 다음 동기화 | 링크·객체 | - |
| `update_dns_record` | 가역·지장 | 링크 | 다음 동기화 | 링크·객체 | - |
| `enable_pool_member` | 가역·무지장 | 복합 | 안정된 뒤 | 상태 전환 | - |
| `disable_pool_member` | 가역·지장 | 복합 | 안정된 뒤 | 상태 전환 | - |
| `promote_database_replica` | 가역·지장 | 속성 | 안정된 뒤 | 재시작·단절 | - |
| `backup_database` | 가역·무지장 | 판단 못 함 | 안정된 뒤 | 재시작·단절 | - |
| `restore_database` | 불가역·손실 | 판단 못 함 | 판단 못 함 | 글 없음 | - |
| `schedule_maintenance` | 가역·무지장 | 복합 | 다음 동기화 | 링크·객체 | - |
| `cancel_maintenance` | 가역·무지장 | 속성 | 곧바로 | 표시 | 예 |
| `set_maintenance_mode` | 가역·무지장 | 속성 | 곧바로 | 표시 | 경계 |
| `exit_maintenance_mode` | 가역·무지장 | 속성 | 곧바로 | 표시 | 예 |
| `acknowledge_alarm` | 가역·무지장 | 속성 | 곧바로 | 표시 | 예 |
| `set_label` | 가역·무지장 | 속성 | 곧바로 | 표시 | 예 |
| `register_unknown_resolution` | 가역·무지장 | 객체 | 다음 동기화 | 링크·객체 | - |
| `power_on` | 가역·무지장 | 속성 | 안정된 뒤 | 상태 전환 | - |
| `power_off` | 가역·지장 | 복합 | 안정된 뒤 | 상태 전환 | - |
| `graceful_shutdown` | 가역·지장 | 속성 | 안정된 뒤 | 상태 전환 | - |
| `restart` | 불가역·지장 | 복합 | 안정된 뒤 | 재시작·단절 | - |
| `power_cycle` | 불가역·지장 | 속성 | 안정된 뒤 | 재시작·단절 | - |
| `force_reset` | 불가역·지장 | 판단 못 함 | 안정된 뒤 | 재시작·단절 | - |
| `inspect_on_site` | 불가역·무지장·물리 | 판단 못 함 | 다음 동기화 | 현장 작업 | - |
