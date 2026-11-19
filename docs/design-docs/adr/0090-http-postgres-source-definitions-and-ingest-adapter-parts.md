---
title: ADR-0090 HTTP와 PostgreSQL 소스 정의의 형식과 어댑터가 쓸 수집 부품
kind: decision
status: active
enforced_by:
  - TLOCK001
  - GOCOV001
  - backend/internal/ingest/adapterdef_test.go
  - backend/internal/ingest/adapterprep_test.go
touches_protected:
  - docs/design-docs/adr/0058-sources-from-db-and-api-worker-ingestion-and-editing-screens.md
  - docs/product-specs/requirements-ingestion.md
  - harness/config/test-lock.json
updates_docs:
  - docs/design-docs/sources-and-adapters.md
  - docs/design-docs/defaults.md
  - docs/guides/ingest-definitions.md
  - docs/product-specs/requirements-ingestion.md
amends:
  - adr: "0058"
    what: 결정 3의 adapter 값을 받는 자리(file, http, postgres. 파일 흐름의 ParseSource는 파일만 받고 ParseAdapterSource가 셋을 받는다)와 미결 표의 종류별 HTTP 본문의 필드 이름
amends_tests: []
decision_hash: c22d9b8d36b8803e
---
# ADR-0090 HTTP와 PostgreSQL 소스 정의의 형식과 어댑터가 쓸 수집 부품

## 맥락

ADR-0086이 수집을 페이지의 흐름으로 다시 설계했고 ADR-0088이 페이지 읽기 포트와 파일 어댑터를 `core.ingest`에 두었다. 이제 HTTP와 PostgreSQL 어댑터를 지을 차례다. 두 어댑터가 서야 할 바닥이 아직 없다. 정의는 `adapter`로 `file`만 받고, 컬럼 경로는 점으로만 나뉘며, 레코드를 행으로 바꾸고 페이지로 자르는 코드는 파일의 바이트를 읽는 쪽에만 있고, 소스를 읽지 못한 일을 설정, 일시적인 문제, 제품의 문제 가운데 무엇으로 알릴 방법이 없고, 사람에게 보이는 글에서 시크릿을 가리는 규칙이 코드에 없다.

이 결정은 어댑터의 구현과 나가는 연결과 조립부를 뺀 그 바닥이다. 어댑터는 뒤의 변경이 짓는다.

## 결정

1. **새 유닛이나 import는 없다.** 모두 `core.ingest` 안의 새 파일이다. 쓰는 표준 라이브러리(`net/url`, `net/netip`, `crypto/x509`, `encoding/pem`)는 제한 목록에 없다. 구조 계약은 바뀌지 않는다.
2. **소스 정의의 형식.** `Source`가 `connection`과 `disabled`를, `Dataset`이 `request`, `records_path`, `pagination`, `post_reads`(HTTP)와 `table`, `sql`, `key_collation`(PostgreSQL)을 가진다. 인증의 종류(`none`, `bearer`, `basic`, `header`, `token_request`)와 페이지네이션의 종류(`none`, `page`, `offset`, `cursor`, `next_url`, `link_header`)마다 읽는 필드가 정해져 있고, 읽지 않는 필드와 다른 어댑터의 필드를 적으면 정의 오류다. 필드의 이름과 뜻은 [소스와 어댑터](../sources-and-adapters.md) 2절에 있다. 정의는 시크릿을 `{"secret_id": …}`로 가리키기만 한다. 검사의 내용은 다음과 같다.
   - 주소: `base_url`은 `http`나 `https`이고 사용자 정보, 질의, 조각이 없다. 요청의 경로는 `/`로 시작하고 `//`, 역슬래시, `?`, `#`, `.`·`..` 단계가 없다. PostgreSQL의 `host`는 이름이나 IP 하나이고 쉼표, `/`, `@`가 들어 있으면 거절하며 `credential`(시크릿)이 없으면 정의 오류다.
   - `connection.headers`는 `Host`, `Authorization`, `Cookie`, `Proxy-*`, `Accept-Encoding`, `Content-Length`, `Transfer-Encoding`, `Connection`을 쓰지 못한다.
   - `{`와 `}`가 든 글(요청의 경로, 질의, 본문, SQL)은 두 번 겹쳐 쓴 것(`{{`, `}}`)만 받는다. `{{credential.<필드>}}`는 `token_request`의 본문의 값 하나 전체일 때만 받는다.
   - `ca_pem`은 인증서만 받는다(개인 키가 든 블록은 정의 오류). 뜻은 "그 인증서만 신뢰한다"이다.
   - 정의에는 서버 인증서의 확인을 끄거나 낮추는 길이 없다(ADR-0091과 맞춘다). HTTP에 `tls_verify` 필드가 없고(적으면 모르는 필드와 같은 정의 오류) PostgreSQL의 `sslmode`는 `verify-full`(기본)과 `disable`만 받는다. `disable`을 실제로 받을지는 운영자 설정(암호화 없는 접속 허용)이 정한다.
   - DB와 API의 데이터셋이 `parser`를 적으면 정의 오류다. 방식은 `snapshot`뿐이고 `capabilities`와 뒤에 올 필드(`keyed_read` 등)는 모르는 필드로 거절한다(지금 그대로).
   - `page` 종류는 `first_page`(0이나 1)를 꼭 적게 한다. 0부터 시작하는 소스를 1부터 읽으면 첫 페이지가 조용히 빠지기 때문이다.
   - 값의 시작 값은 정의가 바꿀 수 있다: 요청 시간 30초, 초당 5회, 응답 하나 16 MiB(압축을 푼 뒤), 재시도 기다림 60초, `max_pages` 1,000, PostgreSQL의 연결 10초, 문장 60초, 잠금 5초.
3. **소스 정의를 읽는 함수가 둘이다.** `ParseSource`는 지금처럼 파일 소스만 받고 다른 `adapter`는 지금의 글(`is not one this build has (only file)`)로 거절한다. `ParseAdapterSource`가 셋을 받는다. 옛 `ingest run`이 `ParseSource`를 계속 쓰므로 `http`나 `postgres` 소스를 받아 `-input`이 없는 데이터셋을 조용히 넘기는 일이 없고, 명령줄 코드를 고치지 않는다. 잠긴 테스트가 그 글을 단언하므로 하나의 읽기가 셋을 받게 하는 것은 테스트를 고쳐야 한다. 서비스와 어댑터 쪽은 `ParseAdapterSource`를 쓴다. 파일 소스는 `connection`을 적지 못한다(입력 폴더와 `connection.files`는 뒤의 변경이다).
4. **새 오류 코드는 별도의 표 `AdapterErrorCodes()`에 둔다.** `ErrorCodes()`의 표는 지금의 정의를 도는 잠긴 테스트가 모든 코드를 사례로 요구하므로, ADR-0088이 `ScaleErrorCodes()`를 둔 것과 같은 이유와 방식이다. 코드는 37개이고 모두 `source.`로 시작하며 새 테스트가 모든 코드를 사례로 덮고 표의 종류와 매개변수와 맞는지 본다. 화면의 한국어 사전을 이 표로 덮는 일은 이 표를 화면에 내보내는 변경이 한다.
5. **경로의 문법은 하나이고 `ParsePath`가 읽는다.** `a.b`, `a.0`, `labels["app.name"]`(따옴표 안의 `\"`와 `\\`가 이스케이프)이고 `[*]`와 `[?이름=값]`은 "아직 지원하지 않음"의 정의 오류(`source.path.unsupported`)이다. 값을 꺼내는 `Pick`은 옛 `pick`과 같은 규칙이다. 파일의 JSON Lines 컬럼은 대괄호가 없는 경로를 지금처럼 `.`로 나누고, 대괄호가 있는 경로는 문법으로 읽되 문법에 맞지 않으면 지금처럼 `.`로 나눈다. 그래서 옛 정의는 같은 결과를 낸다. 달라지는 것은 `x["y"]`꼴 경로뿐이고 그것은 문서가 처음부터 지원한다고 적은 뜻으로 바뀐다. HTTP와 PostgreSQL의 정의는 문법에 맞지 않는 경로를 거절한다.
6. **행 만들기와 페이지 자르기.** `RowBuilder`는 JSON 객체 하나를 JSON Lines의 한 줄과 같은 규칙으로 `Row`로 바꾼다(같은 자연 키, 같은 읽지 못함의 이유, `Where`에 "몇째 페이지의 몇째 레코드"). `PageWriter`는 `pageStream`의 같은 규칙(행 수와 크기로 자르기, 한 레코드는 나누지 않음, 데이터셋의 한도)으로 페이지를 자르고, 레코드마다 받은 "그 뒤의 위치" 토큰을 그 레코드로 끝나는 페이지에 싣는다. 파일 어댑터가 쓰는 `pageStream`을 그대로 쓰며 토큰 자리 하나와 `guard`가 없을 때의 확인만 더했다(파일의 결과는 같다). 규칙이 두 곳에 생기지 않는다.
7. **소스 오류의 세 갈래.** `SourceError`는 갈래(설정, 일시적인 문제, 제품)와 고정 목록의 종류, 사람에게 보이는 글, 요청한 사람만 보는 자세한 글, 안쪽 오류를 가진다. `Error`와 `fmt`의 모든 글쓰기(`%v`, `%+v`, `%#v`)는 글만 낸다. `ClassifyCheckError`와 연결 확인의 글(`plainMessage`)은 이것을 먼저 본다. `CheckResult`에 항목별 결과(`Steps`)를 더한다(기존 필드는 그대로).
8. **가리기.** `SensitiveName`(`password`, `passwd`, `secret`, `token`, `api_key`, `apikey`, `authorization`, `credential`을 이름에 포함하는지. 대소문자 무시, `key` 한 낱말은 아님), `MaskSample`, `MaskPage`, `MaskText`(열어 둔 시크릿의 값을 글에서 지우는 이중 방어), `MaskHeaders`(`Content-Type`, `Content-Length`, `Retry-After`의 값만 보임), `MaskServerURL`(서버가 준 주소의 질의 값과 사용자 정보를 가림).
9. **관측과 미리 읽기의 인터페이스.** 어댑터는 로그를 직접 남기지 않고 `Observer`에 `Event`를 알린다. `Event`는 소스·데이터셋 id, 호스트·포트, 메서드, 질의 없는 경로, 상태, 시도, 시간, 바이트, 페이지, 행 수, 갈래와 종류, 어댑터의 문구만 있고 질의, 헤더, 본문, 문장, 키 값, 시크릿을 담을 자리가 없다(테스트가 필드 목록을 고정한다). `Previewer`는 앞의 몇 페이지와 교환 기록을 돌려주는 선택 인터페이스이고 `PageReader`의 메서드를 늘리지 않는다.
10. **요구사항.** REQ-401은 이 변경으로 구현이 끝나 "예정" 표시를 뗀다. REQ-658(행 만들기, 경로, 페이지 자르기), REQ-659(소스 오류), REQ-660(가리기와 관측)을 더한다. REQ-488의 종류별 필드 이름은 이 결정이 정했다. REQ-486, 487, 489, 490, 491, 655는 어댑터가 서야 끝나므로 표시를 그대로 둔다.

## 이유

- 정의의 형식은 어댑터가 서기 전에 굳어야 한다. 어댑터는 정의의 값을 받아 만들어지므로 파일에서 오든 정의 저장소에서 오든 모른다.
- 규칙을 한 곳에 두면 REQ-635(페이지 크기를 바꿔도 같은 결과)가 어댑터마다 따로 증명되지 않는다.
- 오류의 글과 로그에 비밀이 샐 길을 어댑터 한 곳이 아니라 이 부품의 타입이 막는다. 어댑터가 틀려도 `Event`에는 담을 자리가 없고 `SourceError`는 안쪽 오류를 글로 쓰지 않는다.

## 대안과 기각 사유

- **`ParseSource` 하나가 셋을 받게 한다.** 잠긴 테스트(`deferror_test.go`, `ingesterrors_test.go`)가 "(only file)"의 글을 단언하므로 테스트를 고쳐야 하고, 옛 명령이 새 어댑터의 소스를 조용히 넘길 길도 열린다. 읽는 함수를 나누면 둘 다 필요 없다.
- **새 코드를 `ErrorCodes()`에 넣는다.** 잠긴 테스트가 그 표의 모든 코드가 지금의 정의 사례로 나오기를 요구한다.
- **경로의 옛 방식을 완전히 바꾼다.** 대괄호가 든 옛 경로가 문법에 맞지 않으면 정의가 깨진다. 옛 정의가 그대로 돌아야 한다.
- **`page` 종류의 `first_page`에 기본값을 둔다.** 틀린 기본값은 데이터를 조용히 잃는다.
- **`PageReader`에 미리 읽기 메서드를 더한다.** 파일 어댑터와 모든 가짜가 바뀐다. 선택 인터페이스는 타입 단언으로 묻는다.

## 결과

- 잠긴 테스트의 변경은 없다(`test-lock.json`은 새 테스트의 추가뿐).
- 어댑터, 나가는 연결, 조립부, 정의 저장소의 `source` 종류, 파일 소스의 `connection.files`, 시크릿을 여는 조립은 이 변경에 없다.
- 지금 코드에서 이 부품을 부르는 곳은 테스트뿐이다. 첫 소비자는 HTTP 어댑터다.
- 문서 7.2의 PostgreSQL "준비한 문장 하나" 문장 고침은 그 어댑터의 변경이 한다.
