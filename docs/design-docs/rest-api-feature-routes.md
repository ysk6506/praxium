---
title: REST API 기능마다의 길 — 결정이 낸 기능에 `/v1`의 길이 있는가
kind: reference
status: active
follows:
  - docs/design-docs/adr/0016-frontend-separate-rest-api-contract.md
---
# REST API 기능마다의 길

화면이 할 수 있는 일은 모두 공개된 `/v1`로 되어야 해요(ADR-0016 10번). 이 문서는 결정 기록(ADR-0005~0018)과 규모·정의·저장·받기·계약에 걸친 뒤의 결정(8-21~8-34)이 낸 기능을 하나씩 세고, 그 기능의 길을 적어요. **길이 없는 기능은 "없음"이라고 적어서 빈 곳이 보이게 했어요.** 이 표가 OpenAPI 문서를 쓰기 전에 손으로 만드는 표이고, 계약의 검사 "문서의 기능마다 `/v1`의 길이 있는가"의 바탕이에요(rest-api.md 5-3).

> **길 칸의 쓰는 법.** 길은 `METHOD /v1/경로` 꼴의 절대 경로로 적어요(`GET, POST /v1/...`처럼 방법 여럿도 돼요). 하네스가 이 표를 읽어 계약과 등록부와 맞춰 봐요(`API040`~, ADR-0024).

> **길과 칸은 OpenAPI 문서에서 굳어요.** 길은 `/v1/...`이고 행위는 `:행위` 표기 하나예요. 자원의 자세한 칸은 [자원 (1)](rest-api-resources-data.md), [자원 (2)](rest-api-resources-graph.md), [자원 (3)](rest-api-resources-additions.md)에 있어요. `{kg}`는 KG의 ID이고 운영의 KG는 `main`이에요.

읽는 법: **길** 칸은 이미 문서에 있는 길이에요. **없음**은 길이 아직 없다는 뜻이고, 까닭을 적었어요. **해당 없음**은 API로 낼 기능이 아니라는 뜻이에요(구조, 배포, 내부 동작).

## 1. 규모, 저장, 구조 (ADR-0005, 0006, 0009, 8-21, 8-26, 8-27, 8-33)
| 기능 | 길 |
|---|---|
| KG 자원(운영, 후보, 시험, 시스템), 세대, 읽는 자리 | `GET /v1/kgs`, `GET /v1/kgs/{kg}` |
| 새 KG를 만들기(시험 KG) | 없음. 후보 KG는 후보 실행이 만들어요. 시험 KG를 사람이 만드는 길은 정하지 않았어요 |
| 봉한 버전의 목록과 한 버전(실행기의 의미 버전, 해시 규칙의 버전 포함) | `GET /v1/kgs/{kg}/graph-versions`, `GET /v1/kgs/{kg}/graph-versions/{kg_version_id}` |
| 변경의 이어짐을 번호로 가져가기 | `GET /v1/kgs/{kg}/changes`, `POST /v1/kgs/{kg}/questions/changes` |
| `min_seq`, `wait_timeout`으로 복제본 읽기 | 질의와 읽기의 공통 입력(rest-api.md 3-15) |
| 묻는 쪽을 옛 버전에 고정 | `POST /v1/kgs/{kg}/serving-pins`, `DELETE /v1/kgs/{kg}/serving-pins/{pin_id}` |
| 시점 복구 뒤의 세대 | `as_of`, `cursor`, `event_id`의 칸과 `conflict.generation_changed`. 복구를 여는 길은 없음(운영 행위의 목록이 정해요) |
| KG의 한 범위를 파일로 내보내기(REQ-136) | 없음. 알파 뒤의 일이에요. 저장해 둔 범위의 `bulk-reads`가 가장 가까워요 |
| 모양 카드(연결 수의 분포, 깊이, 묶음의 크기) | 없음. 규모 검증의 도구라서 API로 낼지 정하지 않았어요 |
| 지어낸 세계 생성기 | 해당 없음(제품의 도구) |
| KG를 저장소 사이에서 옮기는 절차(오픈 베타) | 없음. 운영 행위로 낼지는 자기 관측성 문서가 정해요 |
| 메시지 기록 저장소 어댑터(받기, 내보내기)의 연결 | 해당 없음(소스와 배포의 설정). 소스는 `GET /v1/sources`, 설정은 `GET /v1/settings` |
| 오래된 것의 자리, 분석용 저장소 | 해당 없음(배포) |
| 백엔드 구조, 언어, 역할 넷 | 해당 없음 |
| 올리는 중의 맞음(두 버전이 함께 도는 것) | `GET /v1/ops/rollout` |
| 큰 버전(API)을 물리는 기간 | 응답의 `Deprecation`과 `Sunset` 머리, `GET /v1/contract` |

## 2. 모델과 KG에 묻기 (ADR-0007, 0010, 8-29 6, 8-33)
| 기능 | 길 |
|---|---|
| 객체 타입, 링크 타입, 액션 타입, 값 타입, 특성, 층, 식별자 방식 읽기 | `GET /v1/object-types`, `GET /v1/object-types/{type_id}`, `GET /v1/link-types`, `GET /v1/link-types/{link_type_id}`, `GET /v1/link-types/{link_type_id}/bindings`, `GET /v1/action-types`, `GET /v1/action-types/{action_type_id}`, `GET /v1/value-types`, `GET /v1/traits`, `GET /v1/traits/{trait_id}`, `GET /v1/layers`, `GET /v1/identifier-schemes`, `GET /v1/identifier-schemes/{scheme_id}` |
| 타입을 만들고 고치기(UI에서 CRUD) | 브랜치의 항목(`POST /v1/branches/{branch_id}/items`)과 제안 |
| 비슷한 타입 찾기, 겹치는 짝의 목록, 타입의 쓰임 | `POST /v1/type-similarity-checks`, `GET /v1/similar-type-pairs`, `GET /v1/type-usage`, `GET /v1/object-types/{type_id}/usage` |
| 타입 합치기와 올리기(`ext`를 `org`로) | `POST /v1/branches/{branch_id}/type-merges`, `POST /v1/branches/{branch_id}/type-promotions` |
| `scope_keys`를 타입의 속성에 선언하기 | 타입 항목의 칸(브랜치의 항목) |
| 표시 이름과 설명을 언어별로 | 각 자원의 `display_names` |
| 객체 찾기와 한 객체, 링크, 이웃 | `GET /v1/kgs/{kg}/objects`, `GET /v1/kgs/{kg}/objects/{object_id}`, `GET /v1/kgs/{kg}/links`, `GET /v1/kgs/{kg}/links/{link_id}` |
| 여러 객체를 한 번에 읽기 | `POST /v1/kgs/{kg}/objects:batch-get` |
| 닫힌 질의 열셋 | `POST /v1/kgs/{kg}/questions/{question_name}` |
| 노출됨의 둘(`exposed_direct`, `exposed_inherited`) | `impact`의 답 |
| 지정해서 다시 받기와 그 결과 | `POST /v1/kgs/{kg}/refreshes`, `GET /v1/kgs/{kg}/refreshes/{refresh_id}` |
| 기대와 견주기 | `POST /v1/kgs/{kg}/value-checks` |
| MCP의 읽는 면(다시 받기와 견주기 포함) | MCP 툴(rest-api.md 5-6) |
| 이 합침을 되돌리기와 식별 이력 | `GET /v1/kgs/{kg}/identity-events`, `POST /v1/kgs/{kg}/identity-events/{event_id}:revert` |
| 대량 읽기 | `POST /v1/kgs/{kg}/scopes/{scope_id}/bulk-reads`, `GET /v1/jobs/{job_id}/result` |
| 저장해 둔 범위 | `GET, POST /v1/kgs/{kg}/scopes`, `DELETE /v1/kgs/{kg}/scopes/{scope_id}` |

## 3. 받기와 매핑 (ADR-0008, 8-21, 8-29, 8-30, 8-31)
| 기능 | 길 |
|---|---|
| 어댑터, 프리셋, 소스, 데이터셋, 정제, 공급, 기대 조건, 일정 읽기 | `GET /v1/adapters`, `GET /v1/presets`, `GET /v1/sources`, `GET /v1/datasets`, `GET /v1/refinements`, `GET /v1/feeds`, `GET /v1/expectations`, `GET /v1/schedules` |
| 정의의 고치기 | 브랜치의 항목과 제안 |
| 소스 들여다보기, 점검(`branch_id`를 받음) | `POST /v1/sources/{source_id}:introspect`, `POST /v1/sources/{source_id}:introspect:check` |
| 엑셀·CSV 올리기와 칸 견본 | `POST /v1/sources/{source_id}/uploads`, `GET /v1/sources/{source_id}/upload-template` |
| 소스 일시 정지와 다시 켜기, 지금 한 번 | `POST /v1/sources/{source_id}:pause`, `POST /v1/sources/{source_id}:pause:resume`, `POST /v1/schedules/{id}:run-now` |
| 데이터셋의 견본 줄과 컬럼 통계 | `POST /v1/datasets/{dataset_id}:preview` |
| 매핑의 미리 보기와 제안 | `POST /v1/definition-items:preview`, `POST /v1/definition-items:preview:suggest`(`kind: feed`) |
| 타입 쪽에서 보기와 빈 속성의 후보 | `GET /v1/object-types/{type_id}/feed-table`, `GET /v1/object-types/{type_id}/feed-candidates` |
| 견본 문서로 해석기의 후보와 나무 | `POST /v1/parsers:probe` |
| 밀어 넣기(`ingester`) | `POST /v1/sources/{source_id}:push` |
| 수집 봉투(`ingester`) | `POST /v1/ingest/envelopes`, `GET /v1/collectors` |
| 수집기를 등록하고 토큰을 바꾸기 | `ingester` 시스템 계정의 길(`GET /v1/system-accounts`). 수집기 전용의 만들기는 없음 |
| 실행, 단계별 수, 견본, 다시 돌리기 | `GET /v1/sync-runs`, `GET /v1/sync-runs/{run_id}/stages`, `GET /v1/sync-runs/{run_id}/samples`, `POST /v1/sync-runs/{run_id}/replays` |
| 붙잡힌 흐름 풀기와 버리기 | `GET /v1/feed-flows`, `POST /v1/feed-flows/{id}:release`, `POST /v1/feed-flows/{id}:release:discard` |
| 안전장치에 걸린 적용의 확인 | `POST /v1/guard-confirmations` |
| 대기 중인 값(붙을 곳이 없는 값) | `GET /v1/kgs/{kg}/pending-values` |
| 완전성 선언과 상태 | `GET /v1/kgs/{kg}/completeness-declarations`(고치기는 공급 항목) |
| 자동으로 멈춘 완전성 선언을 다시 켜기 | 없음. 길과 역할을 정하지 않았어요 |
| 원본 기록에서 내용을 걷어 내기 | 없음. `admin`의 운영 행위인데 행위의 이름을 자기 관측성 문서가 정해요 |
| 읽지 못한 줄, 줄 틀과 해석기의 목록 | 행 셈은 `GET /v1/sync-runs/{run_id}/stages`, 목록은 `GET /v1/catalog/parsers` |

## 4. 정의의 버전, 브랜치, 에이전트 (ADR-0014, 8-25)
| 기능 | 길 |
|---|---|
| 개요, 묶음, 차이, 항목의 이력 | `GET /v1/definitions-overview`, `GET /v1/definition-sets`, `GET /v1/definition-sets/{from}/diff/{to}`, `GET /v1/definition-items/{item_id}` |
| 브랜치 열기, 항목 쓰기, 한 번에 쓰기 | `GET, POST /v1/branches`, `GET /v1/branches/{branch_id}`, `GET, POST /v1/branches/{branch_id}/items`, `PATCH, DELETE /v1/branches/{branch_id}/items/{item_id}`, `POST /v1/branches/{branch_id}/items:batch` |
| 작은 변경을 제안 하나로 | `POST /v1/proposals:from-items` |
| 본줄기에 맞추기, 충돌 풀기 | `POST /v1/branches/{branch_id}:align`, `GET /v1/branches/{branch_id}/conflicts`, `POST /v1/branches/{branch_id}/conflicts/{conflict_id}:resolve` |
| 제안, 검사, 후보 실행, 미리 돌리기 | `GET, POST /v1/proposals`, `GET /v1/proposals/{proposal_id}`, `GET /v1/proposals/{proposal_id}/checks`, `POST /v1/proposals/{proposal_id}/candidate-runs`, `GET /v1/candidate-runs/{run_id}` |
| 검토, 승인, 줄, 닫기 | `POST /v1/proposals/{proposal_id}:request-review`, `POST /v1/proposals/{proposal_id}:approve`, `POST /v1/proposals/{proposal_id}:reject`, `POST /v1/proposals/{proposal_id}:enqueue`, `POST /v1/proposals/{proposal_id}:close`, `GET /v1/merge-queue` |
| 위험의 등급, 자동 병합의 정책 | `GET /v1/merge-policies`, 고치기는 브랜치의 항목 |
| 묶어서 병합(클로즈 베타 설계) | 해당 없음(서버의 줄 처리). 묶음의 상태는 `merge-queue`에 보여요 |
| 되돌리기, 판으로 돌리기, 급한 되돌리기 | `POST /v1/proposals/{proposal_id}/reverts`, `GET /v1/proposals/{proposal_id}/revert-preview`, `POST /v1/definition-sets/{set_no}/restores` |
| 에이전트 실행 하나가 만든 병합을 크게 되돌리기 | `POST /v1/agent-runs/{agent_run_id}/reverts` |
| 시험 수집(새 소스) | `POST /v1/trial-ingests` |
| 계보와 누가 쓰나 | `GET /v1/definitions-lineage`, `POST /v1/definitions-lineage/what-depends` |
| 정의 내보내기와 들이기 | `POST /v1/definition-sets/{set_no}/exports`, `POST /v1/definition-imports` |
| 저장하지 않고 검사, 미리 보기, 제안 | `POST /v1/definition-items:validate`, `POST /v1/definition-items:validate:preview`, `POST /v1/definition-items:validate:preview:suggest` |
| 혼자 쓰는 시험 배포의 `solo_approval` | `PATCH /v1/settings` |
| 회로 차단(자동 병합을 끄기) | 해당 없음(서버의 동작). 알림과 `merge-policies`에 보여요 |

## 5. 관측, 역량 질문, 권한 (ADR-0011, 0012, 0013, 0015, 8-21 라 2·4)
| 기능 | 길 |
|---|---|
| 건강과 건강 요약 | `GET /v1/kgs/{kg}/health`, `GET /v1/kgs/{kg}/health/{resource_kind}/{resource_id}` |
| 지표, 목표, 알림의 규칙, 알림, 알림의 확인·잠시 끔·넘기기 | `GET /v1/metrics`, `POST /v1/metrics/query`, `GET /v1/objectives`, `GET /v1/alert-rules`, `GET /v1/alerts`, `POST /v1/alerts/{alert_id}:acknowledge`, `POST /v1/alerts/{alert_id}:snooze`, `POST /v1/alerts/{alert_id}:hand-over` |
| 시간줄 | `GET /v1/resources/{resource_kind}/{resource_id}/timeline` |
| 문제 목록과 문제에서 제안 열기 | `GET /v1/kgs/{kg}/issues`, `POST /v1/kgs/{kg}/issues/{issue_id}:open-proposal` |
| KG가 틀렸다고 알리기 | `POST /v1/kgs/{kg}/objects/{object_id}/issue-reports` |
| 역량 질문과 충족률 | `GET /v1/competency-questions`, `POST /v1/kgs/{kg}/questions/competency` |
| 표준 모델이 싣는 물음을 고쳐 쓰기 | 브랜치의 항목(`kind: competency_question`) |
| 본보기 객체에서 역량 질문의 패턴 만들기(REQ-195) | 없음. 길을 정하지 않았어요 |
| 쓰임에서 올라오는 역량 질문의 제안 | 없음. 클로즈 베타의 구현이고 계약의 자리도 정하지 않았어요 |
| 나, 역할, 사람과 무리 찾기 | `GET /v1/me`, `GET /v1/roles`, `GET /v1/directory/subjects` |
| 권한 부여(KG, 객체 조건, 범위 있는 관리의 자리)와 거두기 | `GET, POST /v1/grants`, `POST /v1/grants/{grant_id}:revoke` |
| 시스템 계정(`ingester` 포함), 토큰 바꾸기, 끄기 | `GET, POST /v1/system-accounts`, `POST /v1/system-accounts/{account_id}:rotate-token`, `POST /v1/system-accounts/{account_id}:disable` |
| 담당자 모아 보기 | `GET /v1/owners` |
| 감사 기록, 사용 기록 | `GET /v1/audit-records`, `GET /v1/usage-records` |
| 막힌 일 풀기 | `POST /v1/blocked-items/{id}:resolve` |
| 가려진 것의 수 | 응답의 `hidden`(있음/없음, 범위가 전체이면 수) |

## 6. 프락시움 자신의 운영, 비밀 값 (ADR-0017, 0018, 8-23, 8-24, 8-28)
| 기능 | 길 |
|---|---|
| 떠 있나, 받을 수 있나, 다 떴나 | `GET /v1/health/live`, `GET /v1/health/ready`, `GET /v1/health/started` |
| 진단 | `POST /v1/ops/diagnose` |
| 운영 행위의 목록과 미리 해 보기 | `GET /v1/ops/actions`, `POST /v1/ops/actions/{action}:dry-run` |
| 운영 행위를 요청하기(행위마다 하나) | `POST /v1/ops/actions/{action}/runs` |
| 운영 행위의 승인과 거절(사람만) | `POST /v1/ops/action-runs/{run_id}:approve`, `POST /v1/ops/action-runs/{run_id}:approve:reject` |
| 실행의 목록, 한 건, 취소 | `GET /v1/ops/action-runs`, `GET /v1/ops/action-runs/{run_id}`, `POST /v1/ops/action-runs/{run_id}:cancel` |
| 올리는 중의 상태 | `GET /v1/ops/rollout` |
| 사건 코드, 오류 코드, 기본값 | `GET /v1/catalog/event-codes`, `GET /v1/catalog/error-codes`, `GET /v1/catalog/defaults` |
| 설정을 바꾸고 되돌리기 | `GET, PATCH /v1/settings`, `GET /v1/settings/{name}/revisions` |
| 부품의 건강과 신호 | `GET /v1/kgs/{kg}/health`(시스템 KG), `POST /v1/metrics/query` |
| 프락시움 부품을 운영 KG에 내보내기(기본 꺼짐) | 없음. 설정으로 낼지 정하지 않았어요 |
| 비밀 값을 넣고, 메타 정보를 읽고, 판을 바꾸고 시험하고 물리기, 지우기 | `GET, POST /v1/secrets`, `DELETE /v1/secrets/{secret_id}`, `GET, POST /v1/secrets/{secret_id}/versions`, `POST /v1/secrets/{secret_id}/versions/{version}:check`, `POST /v1/secrets/{secret_id}/versions/{version}:retire` |
| `allowed_hosts` 바꾸기 | `PATCH /v1/secrets/{secret_id}` |
| 으뜸 열쇠의 목록, 다시 감싸기, 물리기 | `GET /v1/master-keys`, `POST /v1/master-keys/{key_id}:rewrap`, `POST /v1/master-keys/{key_id}:rewrap:retire` |
| 풀어 쓴 기록 보기 | `GET /v1/audit-records`(`action`과 `target_id`) |
| 비밀 값으로 소스를 한 번에 멈추기 | 운영 행위(`POST /v1/ops/actions/pause_sources_by_secret/runs`) |
| 확인을 끈 연결의 승인과 기한 | 없음. 소스 항목의 칸과 `admin` 승인의 길을 정하지 않았어요 |
| 임시 콘솔을 내주기 | 해당 없음. 콘솔은 정적 파일이고 공개된 API만 불러요. 켜졌는지는 `GET /v1/capabilities`에 보여요 |

## 7. 계약 전체 (ADR-0016, 8-17, 8-22, 8-32)
| 기능 | 길 |
|---|---|
| 계약 문서, 서버의 기능, 오류의 설명 | `GET /v1/contract`, `GET /v1/capabilities`, `GET /v1/problems/{code}` |
| 목록(정제 연산, 해석기, 받는 방식, 링크의 뜻과 채널, 값 타입, 순수 함수, 질의, 역할, 오류와 사건 코드, 기본값) | `GET /v1/catalog`, `GET /v1/catalog/refinement-ops`, `GET /v1/catalog/parsers`, `GET /v1/catalog/receive-modes`, `GET /v1/catalog/link-meanings`, `GET /v1/catalog/channels`, `GET /v1/value-types`, `GET /v1/catalog/pure-functions`, `GET /v1/catalog/queries`, `GET /v1/roles`, `GET /v1/catalog/error-codes`, `GET /v1/catalog/event-codes`, `GET /v1/catalog/event-codes/{code}`, `GET /v1/catalog/reason-codes`, `GET /v1/catalog/defaults`(rest-api-resources-additions.md 1절) |
| 작업의 진행, 취소, 결과 | `GET /v1/jobs`, `GET /v1/jobs/{job_id}`, `POST /v1/jobs/{job_id}:cancel`, `GET /v1/jobs/{job_id}/result` |
| 사건의 흐름과 이어 받기 | `GET /v1/events/stream`, `GET /v1/events`(`since`로 이어 받아요) |
| 자원마다 할 수 있는 것과 막힌 까닭 | `meta.allowed_actions`, `GET /v1/catalog/reason-codes` |
| 그리기 좋은 꼴과 묶어 요약 | 질의의 `shape` 입력 |

## 8. 알고 가는 한계
- 이 표는 문서에 적힌 기능을 센 것이에요. 구현이 나오면서 기능이 더해지면 이 표에 먼저 줄을 더해요.
- 없음으로 남은 기능은 [자원 (3)](rest-api-resources-additions.md) 5절의 물음과 같이 설계 담당이 정해요.

## 9. 관련 결정과 요구사항
- ADR-0016(9, 10, 11, 12번). 원천: ADR-0005~0015, ADR-0017, ADR-0018.
- REQ-143, REQ-147, REQ-148, REQ-149, REQ-229.
