---
title: ADR-0107 받는 웹훅의 입구 — 정의, 수신 전용 키, 확인 포트, 한도와 신호 표시
kind: decision
status: active
enforced_by:
  - GOARCH010
  - TLOCK001
  - backend/internal/hookin/hka_test.go
  - backend/internal/hookin/hka_server_test.go
  - backend/internal/secrets/hka_webhook_test.go
  - backend/internal/secrets/hka_internal_test.go
  - backend/internal/secrets/hka_more_test.go
  - backend/internal/secretsfile/hka_test.go
  - backend/internal/ingest/hka_webhook_test.go
  - backend/internal/ingest/adapterdef_test.go
  - backend/internal/pgingest/hka_signal_test.go
  - backend/cmd/praxium/hka_secretwebhook_test.go
touches_protected:
  - ARCHITECTURE.md
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - harness/config/test-lock.json
  - docs/design-docs/adr/0018-secrets-and-encryption.md
  - docs/design-docs/glossary.md
  - docs/design-docs/rest-api.md
  - docs/design-docs/rest-api-feature-routes.md
  - docs/design-docs/secrets-and-encryption.md
  - docs/design-docs/sources-and-adapters.md
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements-ingestion.md
amends:
  - adr: "0018"
    what: 받는 웹훅의 수신 전용 키에 한해 API의 확인(복호화 없는 확인과 서명 키의 확인)과 60초 이하의 캐시, 감사 한 줄과 세는 값을 허용한다
amends_tests:
  - go:backend/internal/ingest::TestAdpEveryCodeOfTheAdapterTableIsGivenAtItsPlace
test_change_reason: 받는 웹훅의 정의 오류 코드 열 개를 소스 정의 오류의 표(adapterCodeTable)에 더하므로, 그 표의 모든 코드에 사례가 있는지 보는 이 테스트의 사례 표(adpCases)에 열 개의 사례를 더한다. 옛 사례는 하나도 바꾸거나 줄이지 않았고 단언도 그대로이며, 새 코드마다 그 코드를 내는 정의 하나를 더해 단언이 더 많은 코드에 걸린다.
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/glossary.md
  - docs/design-docs/rest-api.md
  - docs/design-docs/rest-api-feature-routes.md
  - docs/design-docs/secrets-and-encryption.md
  - docs/design-docs/secrets-receive-only-keys.md
  - docs/design-docs/sources-and-adapters.md
  - docs/guides/ingest-definitions.md
  - docs/guides/operate-the-server.md
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements-ingestion.md
decision_hash: 1e6ccbdd13bbda05
---
# ADR-0107 받는 웹훅의 입구 — 정의, 수신 전용 키, 확인 포트, 한도와 신호 표시

다른 제품이 소스의 변화를 알리려고 프락시움을 부르는 길(받는 웹훅)의 첫 조각을 정한다. 이번 변경은 받는 입구의 핵심(인증, 수신 전용 키, 한도, 신호의 표시)이고, 받은 함과 요청 층 필터링, 알람, 보내는 웹훅, 신호가 실행을 여는 일, 서버 배선은 뒤의 변경이다.

## 맥락

지금 프락시움은 소스를 읽으러 나가기만 한다. 소스가 바뀌었다는 것을 소스 쪽에서 알리려면 사람이 아니라 제품이 프락시움을 불러야 하고, 그 호출을 증명하는 값(서명 키나 토큰)을 API 쪽이 확인해야 한다. 그런데 ADR-0018과 ADR-0071은 API에 `open`이 없고(시크릿은 수집 부분만 복호화), 캐시가 없고, 사용마다 한 줄을 남긴다고 정했다. 그대로면 API가 서명을 확인할 수 없다. 반대로 API가 소스 접속 시크릿의 마스터 키를 갖게 하면 API 침해가 모든 소스의 자격 증명 유출이 된다.

받는 입구는 인터넷의 낯선 호출을 직접 받으므로 다른 길보다 방어가 앞서야 한다. 인증 전에 데이터베이스에 닿거나, 모르는 웹훅과 틀린 비밀이 다르게 답하거나, 실패를 잠금으로 세어 맞는 호출까지 막거나, 비밀이 로그에 남는 것이 모두 실제로 있는 실수다.

## 결정

1. **정의 `Source.Webhooks[]`(`core.ingest`).** 필드는 `id`, `kind`(`signal`, `records`, `alarms`), `auth{type, profile}`, `allow_ips`, `limits{rate_per_second, burst, body_bytes}`이다. `records`와 `alarms`는 정의 오류(`source.webhook.kind_not_handled_yet`)이고 `signal`만 처리한다. `url_token`과 `body_only`는 `signal`에만 쓴다. 한도 값은 위쪽 한도(초당 10,000, 버스트 5,000, 본문 4 MiB) 안이고 기본은 50, 500, 256 KiB다. 소스당 32개까지다(기본 8은 설정이 서는 때 이어진다). 정의에는 시크릿도 시크릿 참조도 없다. 파일 어댑터는 웹훅을 둘 수 없다. 정의 오류 코드는 소스 정의 오류의 표(`adapterCodeTable`)에 등록한다.
2. **`ClassifyChange`.** `webhooks[]` 전체(더하기, 삭제, `id`, `kind`, `auth`, `allow_ips`)는 접속 등급이고 `limits`는 올릴 때만 접속 등급이다. 필드마다의 등급은 웹훅 타입의 표(`webhookClasses`)에 두고 새 테스트가 타입의 모든 필드를 읽어 표에 빠진 필드가 없음을 보인다. 소스 타입의 필드(`Source.Webhooks`)는 기존 표(`changeClasses`)에 한 줄을 더한다. 기존의 잠긴 반사 테스트는 바꾸지 않는다.
3. **수신 전용 키(`core.secrets`).** 종류 `hmac_key`(서명 키 그대로)와 `webhook_token`(토큰의 SHA-256 해시만)을 더한다. 키는 서버가 만들고 한 번만 보여 주며, 만드는 길은 `praxium secret webhook-issue|webhook-rotate|webhook-retire|webhook-delete`(`secretCommands`에 함께 있어 기존의 플래그 테스트가 덮는다)다. REST의 `:issue-secret`, `:rotate-secret`은 소스 REST가 선 뒤의 변경이다. 시크릿 id는 (소스, 웹훅)에서 정해지고 `Scope`는 그 소스 하나다.
4. **다른 감싸는 키.** 수신 전용 종류는 소스 접속 시크릿의 마스터 키와 다른 **웹훅 키**(`purpose: webhook_verify`, 키 id `wk-`, 다른 파일 형식과 다른 파일 `<데이터 폴더 식별자>.webhook.key`)로 감싼다. 서비스는 두 키를 서로 대신하지 못하게 목적을 검사한다. API 프로세스는 웹훅 키만 가지면 되고 그러면 접속 시크릿은 풀 수 없다. 토큰의 해시도 같은 기록 형식에 담아 웹훅 키로 감싼다(기록의 무결성 검사를 한 가지로 두기 위해서다).
5. **확인 포트 `Verifier`.** `VerifyHMAC`, `VerifyToken`, `Dummy`. 키도 해시도 내주지 않고 모든 거절(없는 시크릿, 다른 소스, 종류 틀림, 폐기된 버전, 틀린 값)에 같은 `false`를 답하며 저장소와 키의 오류만 오류로 돌려준다. 수신 전용 종류는 `Open`으로 열리지 않고(`secret_receive_only`), 접속용 종류는 확인에 쓰이지 않으며, `Scope`에 그 소스가 없으면 거절한다. 토큰은 양쪽을 프로세스마다 무작위인 키로 HMAC한 뒤 상수 시간으로 견준다. 폐기하지 않은 모든 버전(16개까지)을 확인한다.
6. **캐시(ADR-0018의 "캐시 없음"과 "사용마다 한 줄"을 수신 전용 키에 한해 바꾼다).** 풀린 키를 메모리에 두는 시간은 60초 이하이고, 요청마다 기록의 판(바인딩 태그, 기록을 쓸 때마다 바뀐다)을 견주어 고쳐진 기록은 쓰지 않는다. 캐시를 채울 때 `secret_opened` 감사 한 줄을 남기고 요청마다는 세는 값만 올린다. `Store` 포트는 바꾸지 않는다.
7. **입구 `adapter.hookin`.** `POST /v1/sources/{source_id}/webhooks/{webhook_id}:receive`의 처리기이고 `Directory`(웹훅 표와 소스 상태)와 `SignalSink`(신호 표시)를 받는다. 처리 순서는 (1) 경로를 읽고 웹훅을 메모리에서 찾는다, (2) 주소 허용 목록, (3) 입구 전체의 동시 처리와 본문 메모리 합(넘으면 503)과 웹훅마다의 동시 처리(429), (4) 헤더와 질의로 끝나는 증명은 본문을 읽기 전에, (5) 한도까지만 읽는 본문(읽기 기한 기본 10초 최대 30초, 길이 없으면 한도까지, 선언된 길이가 한도를 넘으면 읽지 않고 413), (6) 서명 확인과 JSON 흘려 읽기 검사, (7) 소스 상태와 웹훅의 빈도 한도, (8) 신호 표시, (9) 202다. **인증이 끝나기 전에는 PostgreSQL에 닿지 않는다.** 서명 프로필은 `standard_webhooks`(`id.timestamp.본문`, SHA-256만, `v1,<base64>` 여러 개 가운데 8개까지, 시각 ±300초)와 `body_only`(약함)다. 모르는 웹훅, 틀린 비밀, 허용 밖 주소는 같은 401과 같은 본문이고 모르는 웹훅도 가장 작은 한도(1 KiB)로 본문을 읽고 더미 비교를 한다. 실패 제한은 사용자 로그인의 제한기를 쓰지 않고 (웹훅, 주소) 토큰 버킷이며 잠금이 없고 맞는 증명은 막히지 않는다. 로그는 (웹훅, 주소, 종류)로 묶어 분당 한 줄이고 비밀, 질의 문자열, 본문이 없다. 입구는 자기 경로(`Match`)의 모든 응답을 자기 처리기로 한다. 서버의 not-found가 경로를 되울리지 않게 하려는 것이다.
8. **문제 코드.** `auth.webhook_invalid`(401), `stale.source_not_receiving`(503), `rate_limited.webhook_rate`(429, `Retry-After`), `invalid_input.webhook_body`(400)를 더하고 본문 크기는 기존 `invalid_input.body_too_large`(413)를 쓴다.
9. **경로는 계획 쪽에.** 서버에 아직 이어지지 않아서 계약이 "구현된 경로"로 서빙하는 경로와 같아야 한다는 검사(API050)에 맞추려고 경로를 계획 등록부(`planned/routes.json`)와 `rest-api-feature-routes.md`의 표에 둔다. 서버 배선 변경이 구현된 경로로 옮긴다.
10. **신호의 표시.** `source_state.signal_pending_at`(마이그레이션 0007, 추가만)을 그 소스의 모든 행에서 비어 있을 때만 채운다(`pgingest.Store.Signal`). 실행을 여는 일은 뒤의 변경이다.
11. **하네스.** 새 단위 `adapter.hookin`을 `harness/config/architecture.yaml`과 `ARCHITECTURE.md`에 더하고(`may_import: core.kernel, core.contract, core.secrets, core.ingest`, 테스트는 서버에 얹어 보려고 `adapter.http`와 `core.model`), `net/http`를 쓸 수 있는 단위에 넣는다. 외부 모듈은 들이지 않는다. 커버리지 하한은 `pnpm ratchet`이 새 단위의 행을 기록한다.

## 잠긴 테스트의 변경

| 키 | 옛 | 새 |
|---|---|---|
| `go:backend/internal/ingest::TestAdpEveryCodeOfTheAdapterTableIsGivenAtItsPlace` | 사례 표(`adpCases`)의 사례가 등록된 정의 오류의 모든 코드를 덮는다(없는 코드가 있으면 실패) | 단언은 그대로. 표에 열 개의 사례(`source.webhook.*`)를 더해, 새로 등록한 코드마다 그 코드를 내는 정의 하나가 있다. 옛 사례는 바뀌지 않았다 |

다른 잠긴 테스트는 바뀌지 않는다. 수신 전용 키의 새 테스트는 고유 접두어(`Hka`)를 쓰고 기존 테스트 파일의 함수는 건드리지 않았다.

## 이유

- 수신 전용 키를 소스 접속 시크릿과 **다른 키로 감싸는 것**이 API에 서명 확인을 주면서 접속 자격 증명을 지키는 가장 작은 길이다. 같은 마스터 키를 API에 주면 한 번의 침해가 모든 소스를 연다.
- 사람이 비밀을 고르지 않으면 Basic이나 토큰의 약한 값을 맞히는 공격이 설 자리가 없다. 그래서 해시를 빠른 해시(SHA-256)로 저장하고 잠금 대신 값싼 버킷으로 CPU를 지킨다.
- 인증 전에 데이터베이스에 닿지 않게 하면 인증 없는 호출이 저장소의 부하가 되지 않는다. 웹훅 표를 메모리에서 찾고 소스 상태는 인증 뒤에 묻는다.
- 한 가지 모양의 401은 웹훅의 있음과 비밀의 어느 부분이 틀렸는지를 알려 주지 않는다. 웹훅 id는 비밀이 아니라서 시간과 큰 본문의 답으로 새는 것은 받아들이는 위험이다.
- 기록의 판으로 캐시를 확인하면 `Store` 포트를 바꾸지 않고 "폐기 뒤 60초"를 지킬 수 있다.

## 대안과 기각 사유

- **API에 마스터 키를 준다.** API 침해가 모든 소스의 접속 자격 증명 유출이 된다. 기각.
- **사용마다 복호화하고 캐시하지 않는다.** 웹훅 한 번마다 복호화와 감사 한 줄이 들어 부하 한 번이 로그 폭주가 된다. 수신 전용 키에 한해 60초 캐시와 세는 값으로 바꾼다.
- **실패를 잠금으로 센다.** 잠금은 맞는 호출까지 막는 서비스 거부가 된다. 값이 서버가 만든 높은 엔트로피라 맞히기 방어가 아니라 CPU 보호가 목적이므로 버킷으로 충분하다.
- **웹훅 정의가 시크릿 id를 가리키게 한다.** 두 웹훅이 한 키를 쓰거나 한 소스가 다른 소스의 키를 가리킬 수 있다. (소스, 웹훅)에서 id를 정해 길을 없앤다.
- **서버 배선까지 한꺼번에 한다.** 입구, 키, 정의, 한도는 각각 검토할 만큼 크다. 배선은 입구가 선 뒤에 한다.
- **별도의 표나 맵으로 잠긴 테스트를 피한다.** 잠긴 테스트가 지키는 성질이 새 코드에 걸리지 않게 된다. 같은 표에 넣고 사례를 더하는 쪽을 택했다.

## 결과

- 알려진 한계: 파일 저장소는 한 프로세스가 데이터 폴더를 잠가서 명령줄이 서버가 도는 동안 키를 바꾸지 못한다. "폐기 뒤 60초"는 저장소를 여럿이 공유하는 배포(PostgreSQL 저장소)에서 의미가 있다.
- 소스당 웹훅 수의 기본 8은 설정이 없어 정의에서는 위쪽 한도 32만 강제한다.
- `url_token`은 질의에 있어 제품 밖의 프록시와 보낸 쪽 설정에 남는다. `signal`에만 허용해서 가장 나쁜 결과를 신호 한 번으로 제한한다.
- 서명이 없는 증명(`header_token`, `basic`, `url_token`)은 재생과 위조를 막지 못한다. HMAC만 막는다.
- 이 입구는 서버에 이어지기 전이라 외부에서 부를 수 없다.
