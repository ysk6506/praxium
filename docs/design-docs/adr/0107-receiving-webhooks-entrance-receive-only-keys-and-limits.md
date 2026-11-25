---
title: ADR-0107 받는 웹훅의 입구 — 정의, 수신 전용 키, 확인 포트, 한도와 신호 표시
kind: decision
status: active
enforced_by:
  - GOARCH010
  - TLOCK001
  - backend/internal/hookin/hka_test.go
  - backend/internal/hookin/hka_server_test.go
  - backend/internal/hookin/hka_flood_test.go
  - backend/internal/hookin/hka_addr_test.go
  - backend/internal/hookin/hka_tables_test.go
  - backend/internal/secrets/hka_webhook_test.go
  - backend/internal/secrets/hka_internal_test.go
  - backend/internal/secrets/hka_more_test.go
  - backend/internal/secrets/hka_fix_test.go
  - backend/internal/secrets/hka_fix_internal_test.go
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
decision_hash: a6a4d3d9c6b90b9e
---
# ADR-0107 받는 웹훅의 입구 — 정의, 수신 전용 키, 확인 포트, 한도와 신호 표시

다른 제품이 소스의 변화를 알리려고 프락시움을 부르는 길(받는 웹훅)의 첫 조각을 정한다. 이번 변경은 받는 입구의 핵심(인증, 수신 전용 키, 한도, 신호의 표시)이고, 받은 함과 요청 층 필터링, 알람, 보내는 웹훅, 신호가 실행을 여는 일, 서버 배선은 뒤의 변경이다.

## 맥락

지금 프락시움은 소스를 읽으러 나가기만 한다. 소스가 바뀌었다는 것을 소스 쪽에서 알리려면 사람이 아니라 제품이 프락시움을 불러야 하고, 그 호출을 증명하는 값(서명 키나 토큰)을 API 쪽이 확인해야 한다. 그런데 ADR-0018과 ADR-0071은 API에 `open`이 없고(시크릿은 수집 부분만 복호화), 캐시가 없고, 사용마다 한 줄을 남긴다고 정했다. 그대로면 API가 서명을 확인할 수 없다. 반대로 API가 소스 접속 시크릿의 마스터 키를 갖게 하면 API 침해가 모든 소스의 자격 증명 유출이 된다.

받는 입구는 인터넷의 낯선 호출을 직접 받으므로 다른 길보다 방어가 앞서야 한다. 인증 전에 데이터베이스에 닿거나, 모르는 웹훅과 틀린 비밀이 다르게 답하거나, 실패를 잠금으로 세어 맞는 호출까지 막거나, 비밀이 로그에 남는 것이 모두 실제로 있는 실수다.

## 결정

1. **정의 `Source.Webhooks[]`(`core.ingest`).** 필드는 `id`, `kind`(`signal`, `records`, `alarms`), `auth{type, profile}`, `allow_ips`, `limits{rate_per_second, burst, body_bytes}`이다. `records`와 `alarms`는 정의 오류(`source.webhook.kind_not_handled_yet`)이고 `signal`만 처리한다. `url_token`과 `body_only`는 `signal`에만 쓴다. 한도 값은 위쪽 한도(초당 10,000, 버스트 5,000, 본문 4 MiB) 안이고 기본은 50, 500, 256 KiB다. 소스당 32개까지다(기본 8은 설정이 서는 때 이어진다). 정의에는 시크릿도 시크릿 참조도 없다. 파일 어댑터는 웹훅을 둘 수 없다. 정의 오류 코드는 소스 정의 오류의 표(`adapterCodeTable`)에 등록한다.
2. **`ClassifyChange`.** `webhooks[]` 전체(더하기, 삭제, `id`, `kind`, `auth`, `allow_ips`)는 접속 등급이고 `limits`는 올릴 때만 접속 등급이다. 필드마다의 등급은 웹훅 타입의 표(`webhookClasses`)에 두고 새 테스트가 타입의 모든 필드를 읽어 표에 빠진 필드가 없음을 보인다. 소스 타입의 필드(`Source.Webhooks`)는 기존 표(`changeClasses`)에 한 줄을 더한다. 기존의 잠긴 반사 테스트는 바꾸지 않는다.
3. **수신 전용 키(`core.secrets`).** 종류 `hmac_key`(서명 키 그대로)와 `webhook_token`(토큰의 SHA-256 해시만)을 더한다. 키는 서버가 만들고 한 번만 보여 주며, 만드는 길은 `praxium secret webhook-issue|webhook-rotate|webhook-retire|webhook-delete`(`secretCommands`에 함께 있어 기존의 플래그 테스트가 덮는다)다. REST의 `:issue-secret`, `:rotate-secret`은 소스 REST가 선 뒤의 변경이다. 시크릿 id는 (소스, 웹훅)에서 정해지고 `Scope`는 그 소스 하나다. id는 `wh_`로 시작하고, 일반 시크릿(`put`, `Create`)은 이 접두사의 id를 거절하며, `webhook-delete`는 수신 전용 종류가 아닌 기록을 지우지 않는다. 값을 담은 `Issued`는 `%v`, `slog`, JSON으로 찍어도 값이 나오지 않는다.
4. **다른 감싸는 키와 다른 파일.** 수신 전용 시크릿은 접속 시크릿의 `secrets.json`이 아니라 같은 폴더의 `webhook-secrets.json`(`format: praxium-webhook-secrets`, `version: 1`)에 둔다. `secrets.json`의 형식과 종류는 그대로여서 이 변경 전의 프로그램이 웹훅 비밀을 만든 뒤에도 그 파일을 그대로 읽고, 웹훅 파일은 그 프로그램이 보지 않는다(옛 프로그램으로 되돌려도 접속 시크릿은 열린다). 서비스는 두 저장소를 따로 받고(`Config.Store`와 `Config.WebhookStore`), 소스 저장소는 수신 전용 종류를, 웹훅 저장소는 그 밖의 종류를 거절한다. 같은 파일에 섞어 `format` 버전을 올리는 길은 되돌릴 때 접속 시크릿 전체가 거절되어 기각했다. 수신 전용 종류는 소스 접속 시크릿의 마스터 키와 다른 **웹훅 키**(`purpose: webhook_verify`, 키 id `wk-`, 다른 파일 형식과 다른 파일 `<데이터 폴더 식별자>.webhook.key`)로 감싼다. 서비스는 두 키를 서로 대신하지 못하게 목적을 검사한다. API 프로세스는 웹훅 파일과 웹훅 키만 열면 되고(접속 시크릿의 파일과 마스터 키는 열지 않는다) 그러면 접속 시크릿은 풀 수 없다. 토큰의 해시도 같은 기록 형식에 담아 웹훅 키로 감싼다(기록의 무결성 검사를 한 가지로 두기 위해서다).
5. **확인 포트 `Verifier`.** `VerifyHMAC`, `VerifyToken`, `Dummy`. 키도 해시도 내주지 않고 모든 거절(없는 시크릿, 다른 소스, 종류 틀림, 폐기된 버전, 틀린 값)에 같은 `false`를 답하며 저장소와 키의 오류만 오류로 돌려준다. 수신 전용 종류는 `Open`으로 열리지 않고(`secret_receive_only`), 접속용 종류는 확인에 쓰이지 않으며, `Scope`에 그 소스가 없으면 거절한다. 토큰은 양쪽을 프로세스마다 무작위인 키로 HMAC한 뒤 상수 시간으로 견준다. 폐기하지 않은 모든 버전(16개까지)을 확인한다.
6. **캐시(ADR-0018의 "캐시 없음"과 "사용마다 한 줄"을 수신 전용 키에 한해 바꾼다).** 풀린 키를 메모리에 두는 시간은 60초 이하이고, 요청마다 기록의 판(바인딩 태그, 기록을 쓸 때마다 바뀐다)을 견주어 고쳐진 기록은 쓰지 않는다. 캐시를 채울 때 `secret_opened` 감사 한 줄을 남기고 요청마다는 세는 값만 올린다. `Store` 포트는 바꾸지 않는다.
7. **입구 `adapter.hookin`.** `POST /v1/sources/{source_id}/webhooks/{webhook_id}:receive`의 처리기이고 `Directory`(웹훅 표와 소스 상태)와 `SignalSink`(신호 표시)를 받는다. 처리 순서는 (1) 경로를 읽고 웹훅을 메모리에서 찾는다, (2) 주소 허용 목록, (3) 입구의 자리: 주소마다의 동시 처리(IPv6는 /64로 묶고 기본 4, 넘으면 429, 자리를 잡기 전에 세기만 한다), 입구 전체의 동시 처리와 본문 메모리 합(넘으면 503), 웹훅마다의 동시 처리(429), (4) 헤더와 질의로 끝나는 증명은 본문을 읽기 전에, (5) 한도까지만 읽는 본문(첫 바이트의 기한 기본 2초, 본문 전체의 읽기 기한 기본 10초 최대 30초, 선언된 길이가 한도를 넘으면 읽지 않고 413), (6) 서명 확인과 JSON 흘려 읽기 검사, (7) 소스 상태와 웹훅의 빈도 한도, (8) 신호 표시, (9) 202다. **인증이 끝나기 전에는 PostgreSQL에 닿지 않는다.** 증명 전에 잡는 것은 작게 둔다. 본문 메모리 몫은 선언한 길이가 아니라 실제로 잡은 버퍼(서명 글이 본문을 복사하는 HMAC 웹훅은 두 배)이고, 64 KiB에서 시작해 읽으며 두 배씩 늘리며 그때마다 몫을 더 잡는다. 읽기 기한을 걸 수 없는 서버(응답 쓰기 래퍼가 `SetReadDeadline`을 넘기지 않아 `ErrNotSupported`가 나는 경우)에서는 호출을 받지 않고 503으로 거절하며 한 번만 기록한다. 기한 없이 본문을 읽지 않는다. 서명 프로필은 `standard_webhooks`(`id.timestamp.본문`, SHA-256만, `v1,<base64>` 여러 개 가운데 8개까지, 시각 ±300초)와 `body_only`(약함)다. 모르는 웹훅, 틀린 비밀, 허용 밖 주소는 같은 401과 같은 본문이고 모르는 웹훅도 가장 작은 한도(1 KiB)로 본문을 읽고 더미 비교를 한다. 실패 제한은 사용자 로그인의 제한기를 쓰지 않고 (웹훅, 주소) 토큰 버킷이며(IPv6 주소는 /64로 묶는다) 잠금이 없고 맞는 증명은 막히지 않는다. 로그는 (웹훅, 주소, 종류)로 묶어 분당 한 줄이고 비밀, 질의 문자열, 본문이 없다. 확인기의 고장(키 파일 문제 등)은 호출마다 Error 한 줄을 내지 않고 웹훅마다 처음 한 줄, 그 뒤 분당 한 줄(그동안의 호출 수와 함께)로 묶는다. 같은 묶음 코드를 쓰고 같은 표 크기 상한을 따른다. 입구는 자기 경로(`Match`)의 모든 응답을 자기 처리기로 한다. 서버의 not-found가 경로를 되울리지 않게 하려는 것이다.
8. **문제 코드.** `auth.webhook_invalid`(401), `stale.source_not_receiving`(503), `rate_limited.webhook_rate`(429, `Retry-After`), `invalid_input.webhook_body`(400)를 더하고 본문 크기는 기존 `invalid_input.body_too_large`(413)를 쓴다.
9. **경로는 계획 쪽에.** 서버에 아직 이어지지 않아서 계약이 "구현된 경로"로 서빙하는 경로와 같아야 한다는 검사(API050)에 맞추려고 경로를 계획 등록부(`planned/routes.json`)와 `rest-api-feature-routes.md`의 표에 둔다. 서버 배선 변경이 구현된 경로로 옮긴다.
10. **신호의 표시.** `source_state.signal_pending_at`(마이그레이션 0007, 추가만)을 그 소스의 모든 행에서 비어 있을 때만 채운다(`pgingest.Store.Signal`). 실행을 여는 일은 뒤의 변경이다.
11. **하네스.** 새 단위 `adapter.hookin`을 `harness/config/architecture.yaml`과 `ARCHITECTURE.md`에 더하고(`may_import: core.kernel, core.contract, core.secrets, core.ingest`, 테스트는 서버에 얹어 보려고 `adapter.http`와 `core.model`), `net/http`를 쓸 수 있는 단위에 넣는다. 외부 모듈은 들이지 않는다. 커버리지 하한은 `pnpm ratchet`이 새 단위의 행을 기록한다.
12. **주소와 프록시.** 운영자가 프록시를 믿는다고 한 배포(`TrustProxy`)에서 호출자의 주소는 `X-Forwarded-For`의 **모든 줄을 이어 붙인 목록의 마지막 항목**이다(프록시가 덧붙인 것이다. 앞의 항목과 앞의 줄은 보낸 쪽이 쓴 말이다). 목록이 비었거나 그 항목이 주소가 아니면 호출은 주소가 없는 것이고, 프록시의 연결 주소로 바꿔 쓰지 않으며, 허용 목록이 있는 웹훅은 그런 호출을 거절한다. `allow_ips`에서 IPv4 주소를 IPv6로 매핑한 꼴(`::ffff:192.0.2.7`과 그 네트워크)은 정의 오류다(입구는 호출자의 주소를 IPv4 꼴로 비교하므로 매핑 꼴의 네트워크는 어떤 호출과도 맞지 않는다). REST와 MCP에도 같은 헤더 규칙의 같은 약점이 있고 그쪽은 이 변경이 고치지 않는다(작은 변경으로 따로 고친다).
13. **TLS 뒤에서만.** 받는 입구는 TLS 뒤에서만 쓴다. 고정 값 인증(`header_token`, `basic`, `url_token`)은 평문에서 망에 그대로 보이고, 서명(`hmac`)도 본문을 가려 주지는 않는다. 평문 HTTP로 듣는 배포를 헬스 상태에 경고로 내는 일은 서버에 잇는 변경(1b)이 한다. 이 변경은 문서에만 적는다.

## 잠긴 테스트의 변경

| 키 | 옛 | 새 |
|---|---|---|
| `go:backend/internal/ingest::TestAdpEveryCodeOfTheAdapterTableIsGivenAtItsPlace` | 사례 표(`adpCases`)의 사례가 등록된 정의 오류의 모든 코드를 덮는다(없는 코드가 있으면 실패) | 단언은 그대로. 표에 열 개의 사례(`source.webhook.*`)를 더해, 새로 등록한 코드마다 그 코드를 내는 정의 하나가 있다. 옛 사례는 바뀌지 않았다 |

다른 잠긴 테스트는 바뀌지 않는다. 수신 전용 키의 새 테스트는 고유 접두어(`Hka`)를 쓰고 기존 테스트 파일의 함수는 건드리지 않았다.

## 이유

- 수신 전용 키를 소스 접속 시크릿과 **다른 키로 감싸는 것**이 API에 서명 확인을 주면서 접속 자격 증명을 지키는 가장 작은 길이다. 같은 마스터 키를 API에 주면 한 번의 침해가 모든 소스를 연다.
- 사람이 비밀을 고르지 않으면 Basic이나 토큰의 약한 값을 맞히는 공격이 설 자리가 없다. 그래서 해시를 빠른 해시(SHA-256)로 저장하고 잠금 대신 값싼 버킷으로 응답과 기록의 양을 줄인다. 버킷은 응답 코드를 바꿀 뿐 본문 읽기와 서명 계산을 막지 않는다. 그 일의 양은 본문 한도와 동시 처리 한도(주소마다, 웹훅마다, 입구 전체)와 읽기 기한이 묶는다.
- 인증 전에 데이터베이스에 닿지 않게 하면 인증 없는 호출이 저장소의 부하가 되지 않는다. 웹훅 표를 메모리에서 찾고 소스 상태는 인증 뒤에 묻는다.
- 한 가지 모양의 401은 웹훅의 있음과 비밀의 어느 부분이 틀렸는지를 알려 주지 않는다. 웹훅 id는 비밀이 아니라서 증명 전의 답으로 웹훅 id의 있음이 드러나는 것은 받아들이는 위험이다. 알려진 길은 (1) 시간(모르는 웹훅, 아는 토큰 틀림, 아는 서명 틀림의 처리 시간 차이는 마이크로초 단위다), (2) 큰 본문의 답(1 KiB가 넘는 본문은 모르는 웹훅이 413으로, 아는 HMAC 웹훅이 401로 답하고, 그 웹훅의 한도를 넘으면 413과 `limit_bytes`로 설정한 한도 값까지 드러난다), (3) 모르는 경로가 한 키(`unknown`)의 실패 버킷을 나눠 써서, 같은 주소에서 모르는 id가 429를 받는 동안 아는 id는 401을 받는 것, (4) `Expect: 100-continue`(모르는 웹훅과 HMAC 웹훅은 `100 Continue`, 아는 헤더 토큰 웹훅은 곧장 401)다. 웹훅 id를 알아도 비밀이 없으면 아무것도 할 수 없다.
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
- 알려진 한계(입구): 여러 주소로 흩어진 호출자는 입구가 막지 못한다. 주소 4개가 각 4개씩 쥐면 한 웹훅의 16칸이, 주소 32개가 쥐면 입구의 128칸이 찬다. 다만 기한(첫 바이트 2초, 바이트를 흘려도 10초)이 지나면 풀리므로 호출자는 연결을 계속 새로 열어야 한다. 이것은 앞단 프록시나 방화벽의 연결 제한이 맡는다. 레코드가 붙는 1b에서 다시 본다.
- 알려진 한계(프록시): 프록시를 믿는다고 알리지 않은 채 프록시 뒤에 두면 모든 호출이 프록시 주소 하나로 보여 입구 전체가 동시 4개로 줄어든다(안전한 쪽으로 실패한다). 프록시 뒤에서는 반드시 알려야 한다.
- 알려진 한계(실패 기록): 실패 제한의 표와 기록의 표는 키 8,192개가 차면 통째로 비워지고, 그때 버킷이 새로 시작한다. 실패 제한은 응답과 기록의 양을 줄이는 장치이고, 일의 양은 주소별 4가 막는다.
- 소스당 웹훅 수의 기본 8은 설정이 없어 정의에서는 위쪽 한도 32만 강제한다.
- `url_token`은 질의에 있어 제품 밖의 프록시와 보낸 쪽 설정에 남는다. `signal`에만 허용해서 가장 나쁜 결과를 신호 한 번으로 제한한다.
- 서명이 없는 증명(`header_token`, `basic`, `url_token`)은 재생과 위조를 막지 못한다. HMAC만 막는다.
- 이 입구는 서버에 이어지기 전이라 외부에서 부를 수 없다.
- 평문 HTTP로는 쓰지 않는다(결정 13). 평문 경고는 1b가 헬스 상태에 낸다.
- 1b가 해야 하는 것: `secrets.Observer`를 이어 `secret_opened` 감사 한 줄이 실제 프로그램에서 나가게 한다(이 변경에서는 선택 항목이고 이어 주는 곳이 없다). 서버의 시간 한도(`ReadHeaderTimeout`뿐이고 연결 수 상한과 `IdleTimeout`이 없다)를 정한다. API 역할에 웹훅 파일과 웹훅 키만 열어 준다. 확인기가 요청마다 `Store.Get`으로 저장소를 읽는 일은 PostgreSQL 저장소가 오면 인증 전에 데이터베이스를 읽는 일이 되므로 판만 값싸게 읽는 방법이나 짧은 캐시를 둔다. 소스가 어느 KG에도 놓이지 않았을 때 `Signal`이 아무것도 적지 않고 202를 돌려주는 길이 `Receiving()`의 `stale.source_not_receiving`으로 막히는지 확인한다. 확인기가 캐시를 채울 때 락을 쥔 채 복호화한다(`material`과 `fill`). 부하 테스트에서 본다.
- `CheckWebhookEventID`(이벤트 id를 적는 자리의 검사)는 이벤트 id 필드가 서는 뒤 변경(PR 2)이 쓴다. 지금은 함수와 테스트만 있다.
- 서버를 멈추지 않고는 비밀을 바꾸지 못하고(파일 저장소), 비밀의 파일과 키 파일은 따로 백업한다. `webhook-secrets.json`은 웹훅 키 없이는 열리지 않는다.
