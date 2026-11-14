---
title: REST API 기능별 경로 — 결정이 정한 기능마다 `/v1` 경로가 있는가
kind: reference
status: active
follows:
  - docs/design-docs/adr/0016-frontend-separate-rest-api-contract.md
---
# REST API 기능별 경로

화면이 할 수 있는 모든 작업은 공개된 `/v1`로 가능해야 해요(ADR-0016 10번). 이 문서는 결정 기록(ADR-0005~0018)과 규모·정의·저장·수집·계약에 걸친 이후의 결정(8-21~8-34)이 정한 기능을 하나씩 열거하고, 그 기능의 경로를 적어요. **경로가 없는 기능은 "없음"이라고 적어서 빠진 부분이 보이게 했어요.** 이 표는 OpenAPI 문서를 작성하기 전에 수작업으로 만드는 표이고, 계약 검사 "문서의 기능마다 `/v1` 경로가 있는가"의 기준이에요(rest-api.md 5-3).

> **경로 열의 작성 방법.** 경로는 `METHOD /v1/경로` 형식의 절대 경로로 적어요(`GET, POST /v1/...`처럼 메서드가 여럿이어도 돼요). 하네스가 이 표를 읽어 계약과 등록부와 대조해요(`API040`~, ADR-0024).

> **경로와 필드는 OpenAPI 문서에서 확정돼요.** 경로는 `/v1/...`이고 액션은 `:액션` 표기 하나예요. 자원의 상세 필드는 [자원 (1)](rest-api-resources-data.md), [자원 (2)](rest-api-resources-graph.md), [자원 (3)](rest-api-resources-additions.md)에 있어요. `{kg}`는 KG의 ID이고 운영 KG는 `main`이에요.

읽는 방법: **경로** 열은 이미 문서에 정의된 경로예요. **없음**은 경로가 아직 없다는 의미이고, 이유를 적었어요. **해당 없음**은 API로 제공할 기능이 아니라는 의미예요(구조, 배포, 내부 동작).

## 1. 규모, 저장, 구조 (ADR-0005, 0006, 0009, 8-21, 8-26, 8-27, 8-33)
| 기능 | 경로 |
|---|---|
| KG 자원(운영, 후보, 테스트, 시스템), 세대, 읽기 워터마크 | `GET /v1/kgs`, `GET /v1/kgs/{kg}` |
| 새 KG 생성(테스트 KG) | 없음. 후보 KG는 드라이 런이 만들어요. 사람이 테스트 KG를 만드는 방법은 정하지 않았어요 |
| 확정 버전의 목록과 한 버전(실행기의 의미 버전, 해시 규칙의 버전 포함) | `GET /v1/kgs/{kg}/graph-versions`, `GET /v1/kgs/{kg}/graph-versions/{kg_version_id}` |
| 변경 시퀀스를 번호로 조회하기 | `GET /v1/kgs/{kg}/changes`, `POST /v1/kgs/{kg}/queries:changes` |
| `min_seq`, `wait_timeout`으로 복제본 읽기 | 질의와 읽기의 공통 입력(rest-api.md 3-15) |
| 질의를 이전 버전에 고정 | `POST /v1/kgs/{kg}/serving-pins`, `DELETE /v1/kgs/{kg}/serving-pins/{pin_id}` |
| 시점 복구 후의 세대 | `as_of`, `cursor`, `event_id` 필드와 `conflict.generation_changed`. 복구를 시작하는 경로는 없음(운영 액션 목록이 정해요) |
| KG의 한 범위를 파일로 내보내기(REQ-136) | 없음. 알파 이후 과제예요. 저장해 둔 범위의 `bulk-reads`가 가장 가까워요 |
| 분포 프로파일(연결 수의 분포, 깊이, 그룹의 크기) | 없음. 규모 검증 도구라서 API로 제공할지 정하지 않았어요 |
| 합성 세계 생성기 | 해당 없음(제품의 도구) |
| KG를 스토리지 사이에서 이전하는 절차(오픈 베타) | 없음. 운영 액션으로 제공할지는 자기 관측성 문서가 정해요 |
| 메시지 로그 스토리지 어댑터(수집, 내보내기)의 연결 | 해당 없음(소스와 배포의 설정). 소스는 `GET /v1/sources`, 설정은 `GET /v1/settings` |
| 콜드 스토리지, 분석용 스토리지 | 해당 없음(배포) |
| 백엔드 구조, 언어, 역할 4종 | 해당 없음 |
| 롤아웃 중의 정합(두 버전이 함께 실행되는 것) | `GET /v1/ops/rollout` |
| 메이저 버전(API)의 지원 중단 기간 | 응답의 `Deprecation`과 `Sunset` 헤더, `GET /v1/contract` |

## 2. 모델과 KG 질의 (ADR-0007, 0010, 8-29 6, 8-33)
| 기능 | 경로 |
|---|---|
| 객체 타입, 링크 타입, 액션 타입, 값 타입, 특성, 층, 식별자 방식 읽기 | `GET /v1/object-types`, `GET /v1/object-types/{type_id}`, `GET /v1/link-types`, `GET /v1/link-types/{link_type_id}`, `GET /v1/link-types/{link_type_id}/bindings`, `GET /v1/action-types`, `GET /v1/action-types/{action_type_id}`, `GET /v1/value-types`, `GET /v1/traits`, `GET /v1/traits/{trait_id}`, `GET /v1/layers`, `GET /v1/identifier-schemes`, `GET /v1/identifier-schemes/{scheme_id}` |
| 타입 생성과 수정(UI에서 CRUD) | 브랜치의 항목(`POST /v1/branches/{branch_id}/items`)과 제안 |
| 유사 타입 탐지, 겹치는 쌍의 목록, 타입의 사용 현황 | `POST /v1/type-similarity-checks`, `GET /v1/similar-type-pairs`, `GET /v1/type-usage`, `GET /v1/object-types/{type_id}/usage` |
| 타입 병합과 승격(`ext`를 `org`로) | `POST /v1/branches/{branch_id}/type-merges`, `POST /v1/branches/{branch_id}/type-promotions` |
| `scope_keys`를 타입의 속성에 선언하기 | 타입 항목의 필드(브랜치의 항목) |
| 표시 이름과 설명을 언어별로 | 각 자원의 `display_names` |
| 객체 검색과 객체 하나, 링크, 이웃 조회 | `GET /v1/kgs/{kg}/objects`, `GET /v1/kgs/{kg}/objects/{object_id}`, `GET /v1/kgs/{kg}/objects/{object_id}/links`, `GET /v1/kgs/{kg}/links`, `GET /v1/kgs/{kg}/links/{link_id}` |
| 여러 객체를 한 번에 읽기 | `POST /v1/kgs/{kg}/objects:batch-get` |
| 사전 정의 질의 13종 | `POST /v1/kgs/{kg}/queries:find`, `POST /v1/kgs/{kg}/queries:get`, `POST /v1/kgs/{kg}/queries:neighbors`, `POST /v1/kgs/{kg}/queries:containment`, `POST /v1/kgs/{kg}/queries:paths`, `POST /v1/kgs/{kg}/queries:impact`, `POST /v1/kgs/{kg}/queries:exposure`, `POST /v1/kgs/{kg}/queries:versions`, `POST /v1/kgs/{kg}/queries:quality`, `POST /v1/kgs/{kg}/queries:health`(나머지 10종 가운데 7종. `changes`와 `competency`는 아래 해당 행에 있고, `scopes`는 `GET /v1/kgs/{kg}/scopes`) |
| 노출 2종(`exposed_direct`, `exposed_inherited`) | `impact`의 응답 |
| 지정 재수집과 그 결과 | `POST /v1/kgs/{kg}/refreshes`, `GET /v1/kgs/{kg}/refreshes/{refresh_id}` |
| 기대와 대조 | `POST /v1/kgs/{kg}/value-checks` |
| MCP 읽기 인터페이스(재수집과 기대 대조 포함) | MCP 툴(rest-api.md 5-6) |
| 이 병합을 리버트와 식별 이력 | `GET /v1/kgs/{kg}/identity-events`, `POST /v1/kgs/{kg}/identity-events/{event_id}:revert` |
| 대량 읽기 | `POST /v1/kgs/{kg}/scopes/{scope_id}/bulk-reads`, `GET /v1/jobs/{job_id}/result` |
| 저장해 둔 범위 | `GET, POST /v1/kgs/{kg}/scopes`, `DELETE /v1/kgs/{kg}/scopes/{scope_id}` |

## 3. 수집과 매핑 (ADR-0008, 8-21, 8-29, 8-30, 8-31)
| 기능 | 경로 |
|---|---|
| 어댑터, 프리셋, 소스, 데이터셋, 정제, 피드, 기대 조건, 일정 읽기 | `GET /v1/adapters`, `GET /v1/presets`, `GET /v1/sources`, `GET /v1/datasets`, `GET /v1/refinements`, `GET /v1/feeds`, `GET /v1/expectations`, `GET /v1/schedules` |
| 정의 수정 | 브랜치의 항목과 제안 |
| 소스 탐색, 점검(`branch_id`를 받음) | `POST /v1/sources/{source_id}:introspect`, `POST /v1/sources/{source_id}:introspect:check` |
| 엑셀·CSV 업로드와 필드 샘플 | `POST /v1/sources/{source_id}/uploads`, `GET /v1/sources/{source_id}/upload-template` |
| 소스 일시 정지와 재개, 즉시 1회 실행 | `POST /v1/sources/{source_id}:pause`, `POST /v1/sources/{source_id}:pause:resume`, `POST /v1/schedules/{id}:run-now` |
| 데이터셋의 샘플 행과 컬럼 통계 | `POST /v1/datasets/{dataset_id}:preview` |
| 매핑의 미리 보기와 제안 | `POST /v1/definition-items:preview`, `POST /v1/definition-items:preview:suggest`(`kind: feed`) |
| 타입 기준 조회와 빈 속성의 후보 | `GET /v1/object-types/{type_id}/feed-table`, `GET /v1/object-types/{type_id}/feed-candidates` |
| 샘플 문서로 파서 후보와 트리 확인 | `POST /v1/parsers:probe` |
| 푸시 수집(`ingester`) | `POST /v1/sources/{source_id}:push` |
| 수집 엔벨로프(`ingester`) | `POST /v1/ingest/envelopes`, `GET /v1/collectors` |
| 수집기를 등록하고 토큰 교체하기 | `ingester` 서비스 계정의 경로(`GET /v1/system-accounts`). 수집기 전용 생성 경로는 없음 |
| 실행, 단계별 건수, 샘플, 재실행 | `GET /v1/sync-runs`, `GET /v1/sync-runs/{run_id}/stages`, `GET /v1/sync-runs/{run_id}/samples`, `POST /v1/sync-runs/{run_id}/replays` |
| 보류된 스트림의 해제와 폐기 | `GET /v1/feed-flows`, `POST /v1/feed-flows/{id}:release`, `POST /v1/feed-flows/{id}:release:discard` |
| 안전장치가 작동한 적용의 확인 | `POST /v1/guard-confirmations` |
| 대기 중인 값(연결 대상이 없는 값) | `GET /v1/kgs/{kg}/pending-values` |
| 완전성 선언과 상태 | `GET /v1/kgs/{kg}/completeness-declarations`(수정은 피드 항목) |
| 자동으로 중단된 완전성 선언을 다시 활성화하기 | 없음. 경로와 역할을 정하지 않았어요 |
| 원본 레코드에서 내용을 회수하기 | 없음. `admin`의 운영 액션인데 액션 이름은 자기 관측성 문서가 정해요 |
| 파싱하지 못한 행, 라인 템플릿과 파서의 목록 | 행 개수는 `GET /v1/sync-runs/{run_id}/stages`, 목록은 `GET /v1/catalog/parsers` |

## 4. 정의의 버전, 브랜치, 에이전트 (ADR-0014, 8-25)
| 기능 | 경로 |
|---|---|
| 개요, 정의 세트, 차이, 항목의 이력 | `GET /v1/definitions-overview`, `GET /v1/definition-sets`, `GET /v1/definition-sets/{from}/diff/{to}`, `GET /v1/definition-items/{item_id}` |
| 브랜치 생성, 항목 기록, 일괄 기록 | `GET, POST /v1/branches`, `GET /v1/branches/{branch_id}`, `GET, POST /v1/branches/{branch_id}/items`, `PATCH, DELETE /v1/branches/{branch_id}/items/{item_id}`, `POST /v1/branches/{branch_id}/items:batch` |
| 작은 변경을 제안 하나로 | `POST /v1/proposals:from-items` |
| 메인 브랜치 최신화, 충돌 해결 | `POST /v1/branches/{branch_id}:align`, `GET /v1/branches/{branch_id}/conflicts`, `POST /v1/branches/{branch_id}/conflicts/{conflict_id}:resolve` |
| 제안, 검사, 드라이 런 | `GET, POST /v1/proposals`, `GET /v1/proposals/{proposal_id}`, `GET /v1/proposals/{proposal_id}/checks`, `POST /v1/proposals/{proposal_id}/candidate-runs`, `GET /v1/candidate-runs/{run_id}` |
| 검토, 승인, 큐, 닫기 | `POST /v1/proposals/{proposal_id}:request-review`, `POST /v1/proposals/{proposal_id}:approve`, `POST /v1/proposals/{proposal_id}:reject`, `POST /v1/proposals/{proposal_id}:enqueue`, `POST /v1/proposals/{proposal_id}:close`, `GET /v1/merge-queue` |
| 위험 등급, 자동 병합의 정책 | `GET /v1/merge-policies`, 수정은 브랜치의 항목 |
| 배치 병합(클로즈드 베타 설계) | 해당 없음(서버의 큐 처리). 배치의 상태는 `merge-queue`에 표시돼요 |
| 리버트, 리비전으로 복원, 긴급 리버트 | `POST /v1/proposals/{proposal_id}/reverts`, `GET /v1/proposals/{proposal_id}/revert-preview`, `POST /v1/definition-sets/{set_no}/restores` |
| 에이전트 실행 하나가 만든 병합을 일괄 리버트 | `POST /v1/agent-runs/{agent_run_id}/reverts` |
| 트라이얼 수집(새 소스) | `POST /v1/trial-ingests` |
| 계보와 참조 관계 | `GET /v1/definitions-lineage`, `POST /v1/definitions-lineage/what-depends` |
| 정의 내보내기와 가져오기 | `POST /v1/definition-sets/{set_no}/exports`, `POST /v1/definition-imports` |
| 저장하지 않는 검증, 미리 보기, 제안 | `POST /v1/definition-items:validate`, `POST /v1/definition-items:validate:preview`, `POST /v1/definition-items:validate:preview:suggest` |
| 단독 사용 테스트 배포의 `solo_approval` | `PATCH /v1/settings` |
| 서킷 브레이커(자동 병합 중지) | 해당 없음(서버의 동작). 알림과 `merge-policies`에 표시돼요 |

## 5. 관측, 역량 질문, 권한 (ADR-0011, 0012, 0013, 0015, 8-21 라 2·4)
| 기능 | 경로 |
|---|---|
| 헬스 상태와 헬스 상태 요약 | `GET /v1/kgs/{kg}/health`, `GET /v1/kgs/{kg}/health/{resource_kind}/{resource_id}` |
| 지표, 목표, 알림 규칙, 알림, 알림의 확인·스누즈·에스컬레이션 | `GET /v1/metrics`, `POST /v1/metrics/query`, `GET /v1/objectives`, `GET /v1/alert-rules`, `GET /v1/alerts`, `POST /v1/alerts/{alert_id}:acknowledge`, `POST /v1/alerts/{alert_id}:snooze`, `POST /v1/alerts/{alert_id}:hand-over` |
| 타임라인 | `GET /v1/resources/{resource_kind}/{resource_id}/timeline` |
| 문제 목록과 문제에서 제안 생성 | `GET /v1/kgs/{kg}/issues`, `POST /v1/kgs/{kg}/issues/{issue_id}:open-proposal` |
| KG 오류 신고 | `POST /v1/kgs/{kg}/objects/{object_id}/issue-reports` |
| 역량 질문과 충족률 | `GET /v1/competency-questions`, `POST /v1/kgs/{kg}/queries:competency` |
| 표준 모델에 포함된 질문 수정 | 브랜치의 항목(`kind: competency_question`) |
| 예시 객체에서 역량 질문의 패턴 생성(REQ-195) | 없음. 경로를 정하지 않았어요 |
| 사용 이력에서 도출되는 역량 질문의 제안 | 없음. 클로즈드 베타의 구현이고 계약상 위치도 정하지 않았어요 |
| 내 정보, 역할, 사람과 그룹 검색 | `GET /v1/me`, `GET /v1/roles`, `GET /v1/directory/subjects` |
| 권한 부여(KG, 객체 조건, 범위 한정 위임의 소속)와 회수 | `GET, POST /v1/grants`, `POST /v1/grants/{grant_id}:revoke` |
| 서비스 계정(`ingester` 포함), 토큰 교체, 비활성화 | `GET, POST /v1/system-accounts`, `POST /v1/system-accounts/{account_id}:rotate-token`, `POST /v1/system-accounts/{account_id}:disable` |
| 담당자 모아 보기 | `GET /v1/owners` |
| 감사 로그, 사용 이력 | `GET /v1/audit-records`, `GET /v1/usage-records` |
| 차단된 항목 해결 | `POST /v1/blocked-items/{id}:resolve` |
| 읽기 범위에서 제외된 항목 수 | 응답의 `hidden`(있음/없음, 범위가 전체이면 수) |

## 6. 프락시움 자신의 운영, 시크릿 (ADR-0017, 0018, 8-23, 8-24, 8-28)
| 기능 | 경로 |
|---|---|
| 기동 여부, 요청 수신 가능 여부, 기동 완료 여부 | `GET /v1/health/live`, `GET /v1/health/ready`, `GET /v1/health/started` |
| 진단 | `POST /v1/ops/diagnose` |
| 운영 액션의 목록과 미리 실행 | `GET /v1/ops/actions`, `POST /v1/ops/actions/{action}:dry-run` |
| 운영 액션 요청(액션마다 하나) | `POST /v1/ops/actions/{action}/runs` |
| 운영 액션의 승인과 거부(사람만) | `POST /v1/ops/action-runs/{run_id}:approve`, `POST /v1/ops/action-runs/{run_id}:approve:reject` |
| 실행의 목록, 한 건 조회, 취소 | `GET /v1/ops/action-runs`, `GET /v1/ops/action-runs/{run_id}`, `POST /v1/ops/action-runs/{run_id}:cancel` |
| 롤아웃 중의 상태 | `GET /v1/ops/rollout` |
| 이벤트 코드, 오류 코드, 기본값 | `GET /v1/catalog/event-codes`, `GET /v1/catalog/error-codes`, `GET /v1/catalog/defaults` |
| 설정 변경과 리버트 | `GET, PATCH /v1/settings`, `GET /v1/settings/{name}/revisions` |
| 구성 요소의 헬스 상태와 신호 | `GET /v1/kgs/{kg}/health`(시스템 KG), `POST /v1/metrics/query` |
| 프락시움 구성 요소를 운영 KG에 내보내기(기본 비활성) | 없음. 설정으로 제공할지 정하지 않았어요 |
| 시크릿 등록, 메타 정보 조회, 버전 교체와 테스트와 폐기, 삭제 | `GET, POST /v1/secrets`, `DELETE /v1/secrets/{secret_id}`, `GET, POST /v1/secrets/{secret_id}/versions`, `POST /v1/secrets/{secret_id}/versions/{version}:check`, `POST /v1/secrets/{secret_id}/versions/{version}:retire` |
| `allowed_hosts` 변경 | `PATCH /v1/secrets/{secret_id}` |
| 마스터 키의 목록, 재암호화(rewrap), 폐기 | `GET /v1/master-keys`, `POST /v1/master-keys/{key_id}:rewrap`, `POST /v1/master-keys/{key_id}:rewrap:retire` |
| 복호화 이력 조회 | `GET /v1/audit-records`(`action`과 `target_id`) |
| 시크릿 기준으로 소스를 한 번에 중단하기 | 운영 액션(`POST /v1/ops/actions/pause_sources_by_secret/runs`) |
| 검증을 끈 연결의 승인과 기한 | 없음. 소스 항목의 필드와 `admin` 승인 경로를 정하지 않았어요 |
| 임시 콘솔 제공 | 해당 없음. 콘솔은 정적 파일이고 공개된 API만 호출해요. 활성화 여부는 `GET /v1/capabilities`에 표시돼요 |

## 7. 계약 전체 (ADR-0016, 8-17, 8-22, 8-32)
| 기능 | 경로 |
|---|---|
| 계약 문서, 서버의 기능, 오류 설명 | `GET /v1/contract`, `GET /v1/capabilities`, `GET /v1/problems/{code}` |
| 카탈로그(정제 연산, 파서, 수집 방식, 링크 의미와 채널, 값 타입, 순수 함수, 질의, 역할, 오류와 이벤트 코드, 기본값) | `GET /v1/catalog`, `GET /v1/catalog/refinement-ops`, `GET /v1/catalog/parsers`, `GET /v1/catalog/receive-modes`, `GET /v1/catalog/link-meanings`, `GET /v1/catalog/channels`, `GET /v1/value-types`, `GET /v1/catalog/pure-functions`, `GET /v1/catalog/queries`, `GET /v1/roles`, `GET /v1/catalog/error-codes`, `GET /v1/catalog/event-codes`, `GET /v1/catalog/event-codes/{code}`, `GET /v1/catalog/reason-codes`, `GET /v1/catalog/defaults`(rest-api-resources-additions.md 1절) |
| 작업의 진행 상황, 취소, 결과 | `GET /v1/jobs`, `GET /v1/jobs/{job_id}`, `POST /v1/jobs/{job_id}:cancel`, `GET /v1/jobs/{job_id}/result` |
| 이벤트 스트림과 이어서 조회 | `GET /v1/events/stream`, `GET /v1/events`(`since`로 이어서 조회해요) |
| 자원마다 가능한 액션과 불가 사유 | `meta.allowed_actions`, `GET /v1/catalog/reason-codes` |
| 시각화용 형태와 집계 요약 | 질의의 `shape` 입력 |

## 8. 알려진 한계
- 이 표는 문서에 적힌 기능을 열거한 것이에요. 구현이 진행되며 기능이 추가되면 이 표에 먼저 행을 추가해요.
- 없음으로 남은 기능은 [자원 (3)](rest-api-resources-additions.md) 5절의 질문과 함께 설계 담당이 정해요.

## 9. 관련 결정과 요구사항
- ADR-0016(9, 10, 11, 12번). 원천: ADR-0005~0015, ADR-0017, ADR-0018.
- REQ-143, REQ-147, REQ-148, REQ-149, REQ-229.
