---
title: ADR-0093 PostgreSQL 소스 어댑터 — 자연 키로 페이지를 끊어 페이지마다 짧은 읽기 전용 트랜잭션으로 읽고, 드라이버가 스스로 하는 일을 가둔다
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH021
  - GOCOV001
  - TLOCK001
  - backend/internal/pgsource/read_test.go
  - backend/internal/pgsource/types_test.go
  - backend/internal/pgsource/errors_test.go
  - backend/internal/pgsource/behavior_test.go
  - backend/internal/pgsource/scan_test.go
  - backend/internal/pgsource/hardening_test.go
  - backend/internal/egress/tracker_test.go
  - backend/internal/secrets/nul_test.go
  - backend/internal/pgconn/pqgodebug_test.go
  - backend/internal/sourcecheck/hostile_test.go
  - backend/internal/sourcecheck/tls_test.go
amends:
  - adr: "0091"
    what: 다이얼러에 연결 추적 변종(전체 닫기, 바이트 예산, 모두 평문으로 보기)을 더한다. 뒤로 미룬 일의 PostgreSQL 연결 감싸개 함수는 이것으로 닫는다
touches_protected:
  - docs/product-specs/requirements-ingestion.md
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - harness/config/test-lock.json
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/sources-and-adapters.md
  - docs/product-specs/requirements-ingestion.md
amends_tests:
  - go:backend/internal/egress::TestTheCodeHasNoWayAroundTheDefence
  - go:backend/internal/egress::TestTheDialerConnectsToTheLiteralAddressItJudged
test_change_reason: 두 테스트는 egress의 소스 전체(go:embed *.go)를 읽어 방어를 우회하는 글자가 없는지 보므로, 결정 2의 추적 다이얼러(tracker.go)와 오류 종류 KindLimit이 더해지면 지문이 바뀐다. 테스트 파일은 한 글자도 바뀌지 않았고 단언과 기대 값도 그대로이며, 새 소스에서도 두 테스트가 통과한다
decision_hash: 95dbedf3fd334f69
---
# ADR-0093 PostgreSQL 소스 어댑터

## 맥락

소스 정의(ADR-0090)가 읽는 `postgres` 어댑터를 `core.ingest`의 읽기 포트(ADR-0088. `PageReader`, 선택 포트 `Previewer`와 `Observer`) 위에 짓는다. 이 어댑터가 붙는 DB는 제품의 저장 DB가 아니라 **남이 운영하는 원본**이다. 그래서 쓰지 않고, 긴 트랜잭션을 걸지 않고, 원본의 설정을 바꾸지 않는다. 연결은 ADR-0091의 `adapter.egress`를 지난다. ADR-0091이 "뒤로 미룬 일"에 둔 "PostgreSQL 연결 감싸개 함수"와 TLS의 길, 드라이버가 스스로 읽는 환경과 파일(검토 F8)은 이 변경이 실제 드라이버(lib/pq v1.12.3)와 PostgreSQL 18.7로 **원형을 먼저 만들어 확인한 뒤** 정한다.

## 결정

1. **새 유닛 `adapter.pgsource`**(`backend/internal/pgsource`, `may_import: [core.kernel, core.ingest, core.secrets, adapter.egress]`). `third_party`의 `github.com/lib/pq`를 쓸 수 있는 유닛에 더한다(셋째). 새 Go 모듈은 없다. 테스트만 있는 유닛 `adapter.sourcecheck`(결정 17)가 함께 선다. 제품 자신의 DB 풀(`adapter.pgconn`)은 import하지 않는다.
2. **드라이버는 egress의 다이얼러 하나로 연결한다.** `pq.NewConnectorConfig(Config)`로 만든 커넥터에 `Dialer(d)`로 egress의 추적 다이얼러(`Dialer.Tracker`, 결정 8)를 넣고 `sql.OpenDB`로 풀을 만든다. 이름 풀이와 연결, 그리고 취소 요청의 연결도 이 다이얼러만 지난다. TLS 설정은 `egress.TLSConfig(ca_pem)`의 결과를 `*tls.Config`라고 이름 짓지 않고 `pq.RegisterTLSConfig`에 넘기므로 이 유닛은 `crypto/tls`를 import하지 않는다(`restricted_stdlib` 그대로). egress에 PostgreSQL 전용 연결 함수는 더하지 않고, 읽기의 한도를 거는 선택 변종 하나만 더한다(ADR-0091의 뒤로 미룬 일의 감싸개 함수 항목은 이것으로 닫는다).
3. **드라이버에 주는 `sslmode`는 둘뿐이다**: `disable`, 그리고 읽기마다 새로 등록한 `pqgo-<유일한 키>`(읽기가 끝나면 등록을 지운다). 드라이버의 `require`, `prefer`, `verify-ca`는 검증을 끄고 `verify-full`은 홈 폴더의 `root.crt`를 읽으므로 쓰지 않는다. 드라이버가 `MinVersion`을 `SSLMinProtocolVersion`으로 덮어쓰므로 TLS 1.2로 맞춘다.
4. **드라이버가 스스로 읽는 것을 막는다.** `pq.NewConfig`와 연결 문자열을 쓰지 않고 손으로 채운 `Config`만 준다(`PG*` 변수와 서비스 파일을 읽지 않는다). 남는 둘은 파일 경로를 막는 값으로 채운다: 클라이언트 인증서 `~/.postgresql/postgresql.crt`와 키(`SSLCert`, `SSLKey`), `~/.pgpass`(`Passfile`)는 `/dev/null/praxium-none`(없는 파일)이다. 빈 비밀번호는 연결하기 전에 거절한다.
5. **평문**은 egress의 규칙을 따른다. `sslmode=disable`이면 추적 다이얼러가 **모든 연결**을 TLS 없는 연결로 보고 검사한다(맥락의 표시를 달지 않는다). 취소 요청의 연결은 드라이버가 `context.Background()`로 맺어 표시를 달 길이 없는데, 이렇게 하면 첫 연결과 같은 검사(루프백이 아니면 거절)를 지난다. `sslmode`가 TLS이면 변종은 평문으로 보지 않는다.
6. **읽기**는 `table`과 `sql` 둘이고 같은 모양의 문장을 쓴다: `SELECT <컬럼>, <키>::text FROM <표 | (\n<sql>\n) AS q> WHERE <키마다 IS NOT NULL> [AND (키…) > ($1…)] ORDER BY 키… LIMIT n`. 페이지마다 새 트랜잭션 하나(`REPEATABLE READ READ ONLY`)이고 사이에는 아무것도 잡고 있지 않다. 연결은 1개(`SetMaxOpenConns(1)`)다. 식별자는 `pq.QuoteIdentifier`로 따옴표를 하고 키 값은 매개변수로만 넘긴다. 사용자 `sql`은 한 줄 주석으로 끝나도 감싼 줄바꿈 덕에 안전하고, 끝의 `;` 하나는 지운다.
7. **문장은 언제나 준비해서 실행한다**(`PrepareContext` 뒤 `QueryContext`, 코드에서 `session.go` 한 곳). 인자가 없는 `Query`는 드라이버가 단순 프로토콜로 보내 여러 문장을 받아 주기 때문이다. 준비하면 `a; DROP TABLE x`와 감싼 괄호를 닫는 공격이 모두 42601이다.
8. **시간과 바이트의 한도.** 시간은 트랜잭션의 첫 문장 `pg_catalog.set_config('statement_timeout' | 'lock_timeout' | 'idle_in_transaction_session_timeout', ms, true)`로 그 트랜잭션에만 건다. 세션 값은 바뀌지 않는다(트랜잭션 모드의 풀러 뒤에서도 산다). 이것은 서버가 지키는 한도이고 **서버가 말을 안 하면 소용이 없다.** 그래서 클라이언트도 한도를 둔다. 페이지(트랜잭션) 하나의 마감은 `5×문장 시간 + 10초`이고, 마감이나 읽는 쪽의 취소가 오면 드라이버가 서버에 취소 요청을 보내도록 2초(`cancelGrace`)를 주고, 그 뒤에는 서버가 답하든 말든 `context.AfterFunc`가 추적 다이얼러의 모든 연결을 닫는다. 서버의 질의가 멈추는 것은 서버가 취소 요청을 받은 경우이고, 아니면 서버 쪽 문장 시간이 멈춘다. 바이트는 트랜잭션마다 `4×페이지 바이트 한도 + 1MiB`를 읽을 수 있고(연결 직후는 1MiB) 넘으면 그 연결이 `limit`으로 끝나 읽기는 `response_format`이다. 정직한 원본의 트랜잭션은 한 배치(페이지 한도 + 행 하나)와 한 키가 배치를 채웠을 때 그 키의 행(한 페이지까지)을 읽으므로 글로 오는 값의 부피를 더해도 두 페이지쯤이고, 4배는 메시지 머리글과 키 글과 컬럼 이름의 몫까지 여유를 둔 값이다. 이보다 큰 행은 읽지 않는다. 읽기가 끝나면 모든 연결을 닫는다.
9. **키 집합 페이지 끊기와 토큰.** 키의 타입은 원본의 **카탈로그**로 확인한다(`pg_typeof((SELECT q."키" FROM … LIMIT 0))`를 `pg_catalog.pg_type`에 이어 열거형인지, 도메인이면 바탕 타입을 8단계까지 따라간다). 쓸 수 있는 것은 `pg_catalog`의 정수, `numeric`, `text`, `varchar`, `bpchar`, `uuid`, `date`, `timestamp`, `timestamptz`, 열거형, 그 위의 도메인이다. 원형이 왕복을 확인한 `time`과 `bool`도 받는다. `float`, `bytea`, `json`, 배열, 확장의 타입(`citext` 같은)과 이름 있는 그 밖의 타입은 그 타입의 이름을 밝힌 정의 오류다(`request_rejected`). 드라이버가 열거형과 확장의 타입에 같은 빈 이름을 주므로 이름으로는 가를 수 없다. 비교는 키를 서버가 적은 글(`k::text`, `numeric`은 `pg_catalog.trim_scale`)로 왕복해 같은 값이 같은 글이 되게 한다. `key_collation`은 글자 타입 키에만 건다(정수에 걸면 42804).
10. **한 키의 행은 한 페이지에 싣는다.** 배치가 차면 끝의 같은 키 묶음을 버리고 토큰은 그 앞의 키로 한다. 배치 전체가 한 키면 `=`로 그 묶음을 따로 읽는다. 한 키의 행이 한 페이지(`MaxRows`)보다 많으면 데이터셋은 불완전하다(`Completeness`로 이유를 밝히고 그 키 앞까지만 읽는다).
11. **키가 NULL인 행**은 `IS NOT NULL`이 걸러서 페이지에 오지 않는다(첫 페이지에 이 절이 없으면 NULL 키의 행이 마지막에 정렬되어 나온다 — 원형이 확인한 것). 처음부터 읽을 때 한 번 세어(전체 훑기) 키가 없는 행(`missing_natural_key`, 첫 NULL 컬럼과 모두 비었는지)으로 알리고, `ignore_rows_without_key`면 키가 모두 빈 행은 세기만 한다(`no_key`). 미리 보기와 점검은 세지 않는다.
12. **배열은 서버가 JSON으로** 바꾼다(`to_jsonb`). 어느 컬럼이 배열인지는 `SELECT q.* … LIMIT 0`의 컬럼 타입으로 먼저 안다(준비 단계 한 번). `bytea`를 선언하면 정의 오류다.
13. **시크릿**은 여는 포트(`secrets.Opener`)만 받고(쓰는 포트가 없다) `Reveal()`은 `credential.go` 한 곳이다. 종류는 `password`이고 계정과 비밀번호가 비어 있지 않아야 한다. 연결은 열린 시크릿의 주소로, 목적은 읽기에 `run`, 점검과 미리 보기에 `check`다. 비밀번호는 오류, 이벤트, 미리 보기 어디에도 없다.
14. **오류**는 SQLSTATE와 egress의 종류로 `ingest.SourceError`가 된다(아래 표). 드라이버와 서버의 글은 내보내지 않는다. 읽는 쪽의 `ctx`가 끝났으면 `ctx.Err()`를 그대로 준다(취소하면 드라이버는 컨텍스트 오류가 아니라 57014를 주므로 컨텍스트를 먼저 본다). `Detail`(서버의 문장과 위치)은 42, 22, 0A류에만 붙고 요청한 사람만 본다. 1KiB로 자르고 제어 문자를 뺀다. TLS 실패는 드라이버 안에서 일어나 egress의 오류가 아니므로 `egress.Sanitize`로 읽어 `tls_rejected`로 가른다.
15. **`Check`**는 첫 페이지을 실제로 읽고 `Steps`를 채운다: 시크릿, 이름과 연결, TLS, 인증, 문장, 키 컬럼, 첫 페이지. 실패한 단계에서 끝난다. 계정의 쓰기 권한 조사와 소스 탐색은 이 변경에 없다(REQ-490, REQ-489의 남은 부분).
16. **요구사항.** 이 변경이 다 구현한 REQ-655와 어댑터 부분의 REQ-487, REQ-489, REQ-490에서 "예정"을 떼고, 어댑터가 구현한 부분을 새 번호 REQ-670(구현)으로, 남은 부분(쓰기 권한 점검, 소스 탐색, 샘플)을 REQ-671(예정)로 나눈다. 안전 검토가 더한 방어는 REQ-672에서 REQ-679까지다: 제어 문자(672), 연결 추적과 한도(673), 악의적인 서버(674), TLS의 분류(675), 미리 보기의 상한(676), 키 타입(677), `PQGO_DEBUG`(678), 오류 글의 한도와 `pg_catalog`(679).
17. **테스트만 있는 유닛 `adapter.sourcecheck`**(`backend/internal/sourcecheck`). 악의적인 서버(테스트가 고른 바이트를 보내는 루프백의 가짜 PostgreSQL)와 TLS의 정상 경로(진짜 테스트 서버 앞의 TLS 종단 프록시)를 리포 안에서 시험하려면 소켓을 여는 테스트가 있어야 하는데, 제품 코드가 있는 유닛은 `net`과 `crypto/tls`를 import할 수 없다. 그래서 제품 코드가 없는 유닛(`doc.go`의 패키지 절뿐)에 두고 **그 테스트 파일 이름을 `restricted_stdlib`에 적는다**(유닛 이름이 아니라 `fakepg_test.go`, `tlsproxy_test.go` 파일 단위. 이 줄은 규칙을 풀어 주는 설계 결정이고 이 ADR이 이유다). 테스트만 있는 유닛을 하니스가 강제하는 방법은 `pgcheck`와 같다: `may_import: []`라 제품 코드는 아무것도 import하지 못하고, 테스트가 쓰는 유닛은 `test_may_import`에 적는다. 이 유닛을 import하는 유닛은 없다. 인증서는 테스트가 고정한 시드로 만들고(Ed25519, 시스템의 무작위나 `time.Now`를 쓰지 않고 2126년까지 유효) TLS 프록시는 SSLRequest에 `S`로 답해 종단하고 평문으로 진짜 서버에 넘긴다.
18. **드라이버가 패닉해도 읽기 하나만 끝난다.** lib/pq에는 `recover`가 없고, 악의적인 서버는 음수 길이나 짧은 메시지로 패닉을 낼 수 있다. 드라이버를 부르는 곳은 `session.guard`/`catch`(코드에서 한 곳)가 감싸 패닉을 고정된 글의 `response_format` 오류로 바꾸고(패닉의 값은 글, 로그, `Detail` 어디에도 없다) 연결과 풀을 닫고 다시 쓰지 않는다. 닫는 호출도 같은 감싸개를 지나 연결이 죽었어도 실제로 닫는다(시험에서 이 결함을 찾았다). 드라이버는 **우리 고루틴에서만** 부른다: 연결은 `db.Conn(ctx)`로 하나를 붙잡고 있어서, `database/sql`의 연결을 여는 고루틴은 기다리는 호출자가 있을 때만 일하므로 `Connect`를 부르지 않는다. lib/pq의 취소(`watchCancel`→`cancel`)는 연결을 맺고 취소 요청을 쓰고 SSLRequest의 답 1바이트를 읽고 EOF까지 복사할 뿐이라 서버의 메시지를 해석하지 않는다. 그러나 `database/sql`의 `tx.awaitDone`과 `rows.awaitDone`은 컨텍스트가 끝나면 **그들의 고루틴**에서 드라이버의 `Rollback`과 `Close`를 부르고, 그 안에서 서버의 바이트를 해석할 수 있다(남는 위험). 응답이 모양에 맞지 않는 경우도 막는다: 키 하나가 배치를 채웠는데 그 키의 행이 없으면(원본이 달라졌다) 불완전한 수집으로 끝내고, 행의 열 수가 문장과 다르면, NULL 키를 센 결과가 없는 키 열 번호를 가리키면 `response_format`이다.
19. **제어 문자.** NUL은 시크릿 어느 필드에도 들 수 없다(`core.secrets`). 어댑터는 사용자 이름, 비밀번호, 호스트, 데이터베이스 이름에 NUL이나 제어 문자가 있으면 연결하지 않는다(시크릿의 값이면 `secret_unusable`, 정의의 값이면 정의 오류). 드라이버가 시작 패킷을 NUL로 끝나는 글로 쓰기 때문이다. 어댑터의 문장이 부르는 함수는 `pg_catalog.`를 붙여 쓴다(`search_path`로 바꿔 부를 수 없다. 연산자는 이렇게 못 하므로 남는 위험이다).
20. **미리 보기의 상한.** 페이지당 행 수는 `ceil(MaxRows/MaxPages)`이고 전체는 `MaxRows`에서 끊으며, `WithDefaults`는 `MaxPages`를 20으로 자른다.
21. **`PQGO_DEBUG`가 설정되어 있으면 서버가 시작하지 않는다.** 드라이버가 이 변수로 비밀번호와 읽은 행을 표준 오류에 쓰기 때문이다. `PG*` 변수를 막는 곳(`adapter.pgconn`의 `driverEnvNames`)에 더했고, 어댑터도 읽기 전에 한 번 더 막는다.

## 원형으로 확인한 것

| 질문 | 확인 | 설계에 준 결과 |
|---|---|---|
| egress `Dialer`를 드라이버에 넣는 방법 | `NewConnectorConfig` + `Connector.Dialer` + `sql.OpenDB`. 연결과 취소 요청이 모두 지난다(취소 때 다이얼 횟수 1→2). 이름은 다이얼러만 푼다. 정책 거절은 `*egress.Error`로 나온다 | 결정 2 |
| `verify-full`이 쓸 만한가 | 아니다(홈 폴더의 `root.crt`). `require` 등은 `InsecureSkipVerify`. `pqgo-<키>`와 `RegisterTLSConfig`는 복제한 설정에 `MinVersion`, `ServerName`만 덮는다. 맞는 CA는 통과, 틀린 CA와 이름 불일치는 x509 오류(`tls`로 분류), TLS 없는 서버는 `pq.ErrSSLNotSupported` | 결정 3 |
| `crypto/tls`가 필요한가 | 아니다(결과를 이름 짓지 않고 넘긴다) | egress 변경 없음 |
| 드라이버의 패닉 | 음수 길이, 짧은 데이터 행, 모자란 열, 범위 밖의 열 번호는 패닉이거나 모양이 틀린 답이다. `recover`는 우리 쪽에만 있다 | 결정 18 |
| 큰 길이 필드(0x7FFFFFF0) | 드라이버는 읽기 전에 `make([]byte, n)`을 한다(`recvMessage`). 주소 공간에 여유가 있으면 2GiB를 잡아도 쓰지 않은 쪽은 상주하지 않는다(실험: RSS 4MiB). 주소 공간을 1.5GiB로 제한한 자식 프로세스는 `fatal error: out of memory`로 죽고 이것은 `recover`로 잡히지 않는다. 길이만큼 쓴 만큼만 힙이 자란다(40MiB 행의 시험에서 힙이 40MiB 늘고 예산에서 끊었다) | 남는 위험. 시험도 드라이버 변경도 없다 |
| 읽는 환경과 파일 | 손으로 채운 `Config`는 `PG*`와 서비스 파일을 읽지 않는다. 남는 것: `~/.pgpass`, `~/.postgresql/postgresql.crt`와 키(서버가 클라이언트 인증서를 받았다). 없는 파일 경로로 막으면 0개 | 결정 4. `adapter.pgconn`의 "PG* 변수가 있으면 시작하지 않는다"는 `NewConfig`용이라 필요 없다 |
| 키 왕복 | 정수, `numeric`(1.0과 1.50, 20자리, NaN), 글자(유니코드, 따옴표, 역슬래시), `varchar`의 끝 공백, `bpchar`, `uuid`, 날짜(infinity, BC), 시각(마이크로초, infinity), 열거형, 두 컬럼 키가 모두 같은 다음 페이지를 준다 | 결정 9 |
| NULL 키 | 첫 페이지에 조건이 없으면 NULL 행이 나온다 | 결정 11 |
| 콜레이션 | 정수에 `COLLATE "C"`는 42804 | 결정 9 |
| 사용자 SQL을 부분 질의로 | 준비하면 여러 문장과 주입은 모두 42601, 부작용 없음 | 결정 6, 7 |
| 읽기 전용 | `READ ONLY`는 INSERT를 막는다(SQL 함수 안도 25006). `transaction_read_only`를 끄면 25001. **막지 못하는 것**(문서로 읽었고 실행하지 않았다): 이 DB의 표 밖에 효과가 있는 함수(`pg_advisory_lock`, `pg_notify`, `dblink`, 권한이 있으면 `COPY … PROGRAM`) | 결정 15와 남는 위험 |
| 시간 한도 | `set_config(…, true)`: 300ms 뒤 57014, `lock_timeout` 55P03, 세션 값은 그대로 | 결정 8 |
| 취소 | 진행 중 `pg_sleep(20)`을 700ms에 끝내고 서버의 질의도 사라진다. 오류는 컨텍스트 오류가 아니라 57014 | 결정 14 |
| 드라이버가 주는 값 | 아래 표 | 결정 12 |
| 오류 | 서버의 답은 `*pq.Error`와 SQLSTATE(28P01은 비밀번호와 사용자가 같다), 연결 실패는 `*egress.Error` | 결정 14 |

## 이어 읽기 토큰

| 필드 | 내용 |
|---|---|
| `v` | 토큰의 판. 지금 1. 다른 판은 토큰이 아니다 |
| `fp` | 데이터셋의 지문(SHA-256의 앞 16바이트, 16진수). 데이터셋 id, `table`이나 `sql`, 자연 키, `key_collation`을 덮는다. 컬럼 목록과 접속 정보는 덮지 않는다 |
| `k` | 마지막으로 읽은 키의 글(키 컬럼마다. 서버가 적은 글) |

시크릿, 호스트, 비밀번호는 담지 않는다. 읽을 때 `v`, `fp`, 키의 수가 다르거나 키 글이 4 KiB를 넘거나 JSON이 아니거나 토큰이 8 KiB를 넘으면 토큰이 아니고 처음부터 읽는다(오류가 아니다). 쓸 때 8 KiB를 넘으면 토큰을 싣지 않는다. 키 값은 SQL 글에 넣지 않고 매개변수로만 쓴다. 키가 NULL인 행의 페이지에는 토큰이 없다.

## 값을 글로 바꾸는 표

| 서버의 타입 | 글 |
|---|---|
| `NULL` | 값 없음 |
| `int2`, `int4`, `int8` | 십진 표기 |
| `float4`, `float8` | 가장 짧은 십진 표기(`FormatFloat('g', -1, 64)`) |
| `numeric` | 서버가 적은 글 그대로(자릿수와 `NaN` 유지) |
| `bool` | `true`, `false` |
| `text`, `varchar`, `bpchar`, 열거형, `uuid`, `interval` 등 글로 오는 것 | 글(`bpchar`는 채운 공백 포함, UTF-8이 아닌 바이트는 U+FFFD로 바꾼다) |
| `date` | `2006-01-02`. **BC는 음수 해**(기원전 44년 3월 15일은 `-0043-03-15`) |
| `time` | `15:04:05.999999999` |
| `timetz` | 시간대와 함께(`04:05:06+02:00`) |
| `timestamp` | 시간대 없이 `2006-01-02T15:04:05.999999999` |
| `timestamptz` | RFC 3339, UTC |
| `infinity`, `-infinity` | 그대로 `infinity`, `-infinity` |
| `json`, `jsonb` | JSON 값(수는 `UseNumber`라 자릿수를 잃지 않는다). 컬럼 경로가 안쪽을 따라간다 |
| 배열(타입 이름이 `_`로 시작) | 서버가 `to_jsonb`로 만든 JSON. 안의 NULL은 JSON null이라 값 없음 |
| 도메인 | 바탕 타입의 규칙 |
| `bytea` | 선언하면 정의 오류 |

## 오류의 표

| 원인 | 종류 |
|---|---|
| SQLSTATE 28xxx, 42501 | `auth_failed` |
| 3D000, 25006, 42xxx, 22xxx, 0Axxx | `request_rejected`(42, 22, 0A는 `Detail`에 서버의 문장과 위치) |
| 57014 | `timeout` |
| 55P03, 53xxx, 57P03, 40001, 40P01 | `source_busy` |
| 08xxx, 57P01, 57P02, 연결 끊김, EOF, 네트워크 오류 | `connection_lost` |
| `pq.ErrSSLNotSupported`, egress `tls` | `tls_rejected` |
| egress `policy` / `resolve` / `timeout` / `connect` | `policy_denied` / `name_not_found` / `timeout` / `connection_lost` |
| 시크릿이 없거나 범위와 주소가 안 맞거나 종류가 틀림 | `secret_unusable` |
| 그 밖 | `internal` |

## 버린 대안

- **드라이버의 `sslmode=verify-full`**: 홈 폴더의 파일을 읽고 `ca_pem`을 줄 길이 없다.
- **egress에 PostgreSQL 전용 연결 함수**: 커넥터의 `Dialer`가 이미 같은 일을 한다. 읽기의 한도를 거는 선택 변종만 더한다.
- **`pq.NewConfig`와 `PG*` 변수가 있으면 시작하지 않기**(`adapter.pgconn`의 방식): 환경을 읽는 방법을 막지 않고 알아서 피하는 쪽이 더 단단하다.
- **서버 쪽 커서(`DECLARE CURSOR`)나 `COPY`**: 트랜잭션을 읽기 내내 열어 둔다(원본의 vacuum을 막는다).
- **`OFFSET`으로 페이지 나누기**: 원본이 바뀌면 행이 빠지거나 겹치고, 뒤로 갈수록 느리다.
- **페이지 사이의 하나의 긴 스냅숏 트랜잭션**: 원본에 긴 트랜잭션을 거는 일이라 이 어댑터의 전제에 어긋난다. 페이지 사이에 원본이 바뀌면 각 페이지은 그 페이지이 본 상태이고 행은 겹치지 않는다(키가 오름차순이다).
- **문장을 첫 낱말로만 필터링**: 준비와 읽기 전용 트랜잭션이 더 믿을 만해서 거르개는 두지 않았다(REQ-490의 첫 낱말 거르개는 남는 부분).
- **테스트 서버에 TLS를 켜기**: 이 변경의 범위가 아니다(남는 위험).

## 남는 위험

- **TLS를 켠 실제 서버는 테스트하지 못했다.** TLS는 테스트 서버 앞의 종단 프록시(`adapter.sourcecheck`)로 시험한다: 맞는 CA는 행을 읽고, 틀린 CA와 이름 불일치는 `tls_rejected`이고, TLS를 내지 않는 서버에는 평문으로 물러서지 않는다. 서버 자신이 TLS를 켜는 일은 `with-postgres.sh`에 더하지 않았다.
- **큰 길이 필드의 할당.** 악의적인 서버가 0x7FFFFFF0을 보내면 드라이버가 2GiB의 버퍼를 읽기 전에 잡는다. 주소 공간이나 메모리의 한도가 있는 호스트에서는 `fatal error: out of memory`로 프로세스가 죽는다(`recover`로 잡히지 않는다). 정직한 서버만 소스로 쓰거나 호스트의 한도를 넉넉히 두는 것이 지금의 경계이고, 드라이버를 바꾸지 않았다.
- **행 하나의 크기.** 바이트 예산(결정 8)이 행 하나를 `4×페이지 한도 + 1MiB`에서 끊지만 드라이버는 그 행을 한 메시지로 메모리에 잡으므로 한 행이 그 크기까지는 메모리에 있다.
- **SCRAM의 반복 횟수.** 서버가 매우 큰 반복 횟수를 주면 비밀번호 계산이 CPU를 오래 쓴다. 연결 시간(`connect_timeout`)이 끊지만 그동안 계산은 돈다(검토 P4).
- **평문과 MD5 인증.** `sslmode=disable`인 소스(루프백이거나 운영자가 허용한 곳)는 서버가 요구하면 비밀번호가 평문이나 MD5로 간다(검토 P9). TLS가 아닌 길은 운영자의 선택이다.
- **연산자의 `search_path`.** 함수는 `pg_catalog.`로 부르지만 연산자는 그렇게 쓸 수 없어 원본 계정의 `search_path`로 바꿔 놓은 연산자가 불릴 수 있다(검토 P13). 경계는 읽기 전용 계정이다.
- **정의를 편집할 권한은 원본에 묶인 시크릿을 같은 출처 안에서 쓸 권한이다.** 정의의 `sql`은 그 시크릿의 계정으로 실행된다.
- **`PQGO_DEBUG`.** 서버 시작 때만 막는다. 실행 중에 환경이 바뀌는 길은 이 변경이 막지 않는다(어댑터가 읽기마다 다시 보므로 그때는 읽지 않는다).
- **`database/sql`의 `awaitDone` 고루틴**이 컨텍스트가 끝날 때 드라이버의 `Rollback`과 `Close`를 우리가 가진 고루틴이 아닌 곳에서 부르고, 그 안의 서버 바이트 해석이 패닉하면 `recover`가 없다(결정 18).
- **사용자 `sql`은 읽기 전용 트랜잭션이 막지 못하는 함수를 부를 수 있다**(권고 잠금, `pg_notify`, `dblink`, 권한이 있는 계정의 `COPY … PROGRAM`, 시간 한도를 `set_config`로 푸는 일). 경계는 소스 쪽의 읽기 전용 계정이다. 문장은 정의를 편집할 권한이 있는 사람이 쓴다.
- **비밀번호의 사본이 힙에 남는다**(`Config`의 문자열). 지우지 못한다.
- **TLS 설정 등록소는 프로세스 전역이다.** 키를 읽기마다 새로 만들어 부딪히지 않고 닫을 때 지운다.
- **NULL 키를 세는 일은 전체 훑기다.** 큰 표에서는 읽기를 시작할 때 한 번 드는 비용이다.
- **키 글의 같음은 결정적 콜레이션을 가정한다.** 비결정적 콜레이션(대소문자를 같게 보는 것)이면 서로 다른 글이 한 키로 비교되어 행이 빠질 수 있다.
- **`trim_scale`은 PostgreSQL 13 이상이다.** 제품의 기준은 18이지만 더 낮은 소스의 `numeric` 키는 읽지 못한다.
- **표 이름은 쓴 그대로**(따옴표를 하므로 대소문자를 구분한다).
- **줄 번호와 페이지 번호는 이어 읽을 때 다시 1부터다.**
- **페이지 사이에는 원본이 바뀔 수 있다**(각 페이지은 한 시점이다). 수집의 한 번이 하나의 스냅숏이 되지는 않는다.
- **`MaxConns`가 1이라 페이지 읽기는 순차다.**

## 뒤로 미룬 일

- **제품 DB의 주소와 서버의 듣는 주소를 egress의 `Deny`에 자동으로 더하기**(검토 C3). 운영자가 `Deny`로 넣는다.
- **동시 읽기와 속도의 한도.**
- **실행의 고루틴마다 `recover`를 두기**(결정 18의 `awaitDone`이 닿는 곳).
- **이어 읽기 토큰은 서버의 저장소에서만 받기.** 지금은 호출자가 주는 토큰을 검증해서 읽는다.
- **`SourceError.Err`를 로그에 남기지 않기.** 원인 오류가 글에 드러나지 않게 하는 일은 호출하는 쪽의 규칙이다.
- **`go/ast`로 허용 목록을 훑는 검사**(검토 C1). 지금은 글 훑기 테스트가 지킨다.

## 결과

- 새 유닛 둘(`adapter.pgsource`, 테스트만 있는 `adapter.sourcecheck`), `third_party`의 한 줄, `restricted_stdlib`의 파일 단위 줄 셋, 커버리지 하한의 새 키(도구로), 새 테스트의 잠금이 더해진다.
- 잠긴 테스트의 글은 바꾸지 않는다. 다만 egress의 소스를 품는 잠긴 테스트 둘(`TestTheCodeHasNoWayAroundTheDefence`, `TestTheDialerConnectsToTheLiteralAddressItJudged`)은 품은 소스가 바뀌어 지문만 새로 잠근다(`amends_tests`). 새 소스에서도 두 테스트는 그대로 통과한다. egress를 고치는 변경은 앞으로도 이렇게 결정 기록을 남긴다.
- ADR-0091의 "뒤로 미룬 일" 가운데 PostgreSQL 연결 감싸개 함수는 추적 다이얼러(`Dialer.Tracker`)로 닫힌다(결정 2).
- 조립부와 소스 탐색, 쓰기 권한 점검은 이어지는 변경이다.
