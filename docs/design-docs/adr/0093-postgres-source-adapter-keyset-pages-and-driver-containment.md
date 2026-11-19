---
title: ADR-0093 PostgreSQL 소스 어댑터 — 자연 키로 쪽을 끊어 쪽마다 짧은 읽기 전용 트랜잭션으로 읽고, 드라이버가 스스로 하는 일을 가둔다
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
touches_protected:
  - docs/product-specs/requirements-ingestion.md
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - harness/config/test-lock.json
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/sources-and-adapters.md
  - docs/product-specs/requirements-ingestion.md
decision_hash: 162c872b1312287d
---
# ADR-0093 PostgreSQL 소스 어댑터

## 맥락

소스 정의(ADR-0090)가 읽는 `postgres` 어댑터를 `core.ingest`의 읽기 포트(ADR-0088. `PageReader`, 선택 포트 `Previewer`와 `Observer`) 위에 짓는다. 이 어댑터가 붙는 DB는 제품의 저장 DB가 아니라 **남이 운영하는 원본**이다. 그래서 쓰지 않고, 긴 트랜잭션을 걸지 않고, 원본의 설정을 바꾸지 않는다. 연결은 ADR-0091의 `adapter.egress`를 지난다. ADR-0091이 "뒤로 미룬 일"에 둔 "PostgreSQL 연결 감싸개 함수"와 TLS의 길, 드라이버가 스스로 읽는 환경과 파일(검토 F8)은 이 변경이 실제 드라이버(lib/pq v1.12.3)와 PostgreSQL 18.7로 **원형을 먼저 만들어 확인한 뒤** 정한다.

## 결정

1. **새 유닛 `adapter.pgsource`**(`backend/internal/pgsource`, `may_import: [core.kernel, core.ingest, core.secrets, adapter.egress]`). `third_party`의 `github.com/lib/pq`를 쓸 수 있는 유닛에 더한다(셋째). 새 Go 모듈은 없다. 제품 자신의 DB 풀(`adapter.pgconn`)은 import하지 않는다.
2. **egress에 새 함수를 더하지 않는다.** 드라이버는 `pq.NewConnectorConfig(Config)`로 만든 커넥터에 `Dialer(d)`로 egress의 `Dialer`를 넣고 `sql.OpenDB`로 풀을 만든다. 이름 풀이와 연결, 그리고 취소 요청의 연결도 이 다이얼러만 지난다. TLS 설정은 `egress.TLSConfig(ca_pem)`의 결과를 `*tls.Config`라고 이름 짓지 않고 `pq.RegisterTLSConfig`에 넘기므로 이 유닛은 `crypto/tls`를 import하지 않는다(`restricted_stdlib` 그대로).
3. **드라이버에 주는 `sslmode`는 둘뿐이다**: `disable`, 그리고 읽기마다 새로 등록한 `pqgo-<유일한 키>`(읽기가 끝나면 등록을 지운다). 드라이버의 `require`, `prefer`, `verify-ca`는 검증을 끄고 `verify-full`은 홈 폴더의 `root.crt`를 읽으므로 쓰지 않는다. 드라이버가 `MinVersion`을 `SSLMinProtocolVersion`으로 덮어쓰므로 TLS 1.2로 맞춘다.
4. **드라이버가 스스로 읽는 것을 막는다.** `pq.NewConfig`와 연결 문자열을 쓰지 않고 손으로 채운 `Config`만 준다(`PG*` 변수와 서비스 파일을 읽지 않는다). 남는 둘은 파일 경로를 막는 값으로 채운다: 클라이언트 인증서 `~/.postgresql/postgresql.crt`와 키(`SSLCert`, `SSLKey`), `~/.pgpass`(`Passfile`)는 `/dev/null/praxium-none`(없는 파일)이다. 빈 비밀번호는 연결하기 전에 거절한다.
5. **평문**은 egress의 규칙을 따른다. `sslmode=disable`이면 커넥터를 감싸 연결 맥락에 `egress.WithPlaintext`를 단다. 취소 요청의 연결은 드라이버가 `context.Background()`로 맺으므로 표시가 없고, 그래서 루프백이 아닌 곳에서는 거절된다(안전한 쪽의 실패. 남는 위험).
6. **읽기**는 `table`과 `sql` 둘이고 같은 모양의 문장을 쓴다: `SELECT <컬럼>, <키>::text FROM <표 | (\n<sql>\n) AS q> WHERE <키마다 IS NOT NULL> [AND (키…) > ($1…)] ORDER BY 키… LIMIT n`. 쪽마다 새 트랜잭션 하나(`REPEATABLE READ READ ONLY`)이고 사이에는 아무것도 잡고 있지 않다. 연결은 1개(`SetMaxOpenConns(1)`)다. 식별자는 `pq.QuoteIdentifier`로 따옴표를 하고 키 값은 매개변수로만 넘긴다. 사용자 `sql`은 한 줄 주석으로 끝나도 감싼 줄바꿈 덕에 안전하고, 끝의 `;` 하나는 지운다.
7. **문장은 언제나 준비해서 실행한다**(`PrepareContext` 뒤 `QueryContext`, 코드에서 `session.go` 한 곳). 인자가 없는 `Query`는 드라이버가 단순 프로토콜로 보내 여러 문장을 받아 주기 때문이다. 준비하면 `a; DROP TABLE x`와 감싼 괄호를 닫는 공격이 모두 42601이다.
8. **시간 한도**는 트랜잭션의 첫 문장 `set_config('statement_timeout' | 'lock_timeout' | 'idle_in_transaction_session_timeout', ms, true)`로 그 트랜잭션에만 건다. 세션 값은 바뀌지 않는다(트랜잭션 모드의 풀러 뒤에서도 산다). 쪽 하나의 총 시간은 `5×문장 시간 + 10초`다. 취소하면 서버의 질의도 멈춘다(`pg_stat_activity`로 확인).
9. **키 집합 쪽 끊기와 토큰.** 키로 쓸 수 있는 타입은 정수, `numeric`, `text`, `varchar`, `bpchar`, `uuid`, `date`, `timestamp`, `timestamptz`, `time`, `bool`, 열거형이다. `float`, `bytea`, `json`, 배열과 이름 있는 그 밖의 타입은 정의 오류다(`request_rejected`). 비교는 키를 서버가 적은 글(`k::text`, `numeric`은 `trim_scale`)로 왕복해 같은 값이 같은 글이 되게 한다. `key_collation`은 글자 타입 키에만 건다(정수에 걸면 42804).
10. **한 키의 행은 한 쪽에 싣는다.** 배치가 차면 끝의 같은 키 묶음을 버리고 토큰은 그 앞의 키로 한다. 배치 전체가 한 키면 `=`로 그 묶음을 따로 읽는다. 한 키의 행이 한 쪽(`MaxRows`)보다 많으면 데이터셋은 불완전하다(`Completeness`로 이유를 밝히고 그 키 앞까지만 읽는다).
11. **키가 NULL인 행**은 `IS NOT NULL`이 걸러서 쪽에 오지 않는다(첫 쪽에 이 절이 없으면 NULL 키의 행이 마지막에 정렬되어 나온다 — 원형이 확인한 것). 처음부터 읽을 때 한 번 세어(전체 훑기) 키가 없는 행(`missing_natural_key`, 첫 NULL 컬럼과 모두 비었는지)으로 알리고, `ignore_rows_without_key`면 키가 모두 빈 행은 세기만 한다(`no_key`). 미리 보기와 점검은 세지 않는다.
12. **배열은 서버가 JSON으로** 바꾼다(`to_jsonb`). 어느 컬럼이 배열인지는 `SELECT q.* … LIMIT 0`의 컬럼 타입으로 먼저 안다(준비 단계 한 번). `bytea`를 선언하면 정의 오류다.
13. **시크릿**은 여는 포트(`secrets.Opener`)만 받고(쓰는 포트가 없다) `Reveal()`은 `credential.go` 한 곳이다. 종류는 `password`이고 계정과 비밀번호가 비어 있지 않아야 한다. 연결은 열린 시크릿의 주소로, 목적은 읽기에 `run`, 점검과 미리 보기에 `check`다. 비밀번호는 오류, 이벤트, 미리 보기 어디에도 없다.
14. **오류**는 SQLSTATE와 egress의 종류로 `ingest.SourceError`가 된다(아래 표). 드라이버와 서버의 글은 내보내지 않는다. 읽는 쪽의 `ctx`가 끝났으면 `ctx.Err()`를 그대로 준다(취소하면 드라이버는 컨텍스트 오류가 아니라 57014를 주므로 컨텍스트를 먼저 본다). `Detail`(서버의 문장과 위치)은 42, 22, 0A류에만 붙고 요청한 사람만 본다.
15. **`Check`**는 첫 쪽을 실제로 읽고 `Steps`를 채운다: 시크릿, 이름과 연결, TLS, 인증, 문장, 키 컬럼, 첫 쪽. 실패한 단계에서 끝난다. 계정의 쓰기 권한 조사와 소스 탐색은 이 변경에 없다(REQ-490, REQ-489의 남은 부분).
16. **요구사항.** 이 변경이 다 구현한 REQ-655와 어댑터 부분의 REQ-487, REQ-489, REQ-490에서 "예정"을 떼고, 남은 부분(쓰기 권한 점검, 소스 탐색, 샘플)은 새 번호 REQ-670으로 나눈다. REQ-671은 이 변경이 구현한 쪽 끊기와 방어이다.

## 원형으로 확인한 것

| 질문 | 확인 | 설계에 준 결과 |
|---|---|---|
| egress `Dialer`를 드라이버에 넣는 길 | `NewConnectorConfig` + `Connector.Dialer` + `sql.OpenDB`. 연결과 취소 요청이 모두 지난다(취소 때 다이얼 횟수 1→2). 이름은 다이얼러만 푼다. 정책 거절은 `*egress.Error`로 나온다 | 결정 2 |
| `verify-full`이 쓸 만한가 | 아니다(홈 폴더의 `root.crt`). `require` 등은 `InsecureSkipVerify`. `pqgo-<키>`와 `RegisterTLSConfig`는 복제한 설정에 `MinVersion`, `ServerName`만 덮는다. 맞는 CA는 통과, 틀린 CA와 이름 불일치는 x509 오류(`tls`로 분류), TLS 없는 서버는 `pq.ErrSSLNotSupported` | 결정 3 |
| `crypto/tls`가 필요한가 | 아니다(결과를 이름 짓지 않고 넘긴다) | egress 변경 없음 |
| 읽는 환경과 파일 | 손으로 채운 `Config`는 `PG*`와 서비스 파일을 읽지 않는다. 남는 것: `~/.pgpass`, `~/.postgresql/postgresql.crt`와 키(서버가 클라이언트 인증서를 받았다). 없는 파일 경로로 막으면 0개 | 결정 4. `adapter.pgconn`의 "PG* 변수가 있으면 시작하지 않는다"는 `NewConfig`용이라 필요 없다 |
| 키 왕복 | 정수, `numeric`(1.0과 1.50, 20자리, NaN), 글자(유니코드, 따옴표, 역슬래시), `varchar`의 끝 공백, `bpchar`, `uuid`, 날짜(infinity, BC), 시각(마이크로초, infinity), 열거형, 두 컬럼 키가 모두 같은 다음 쪽을 준다 | 결정 9 |
| NULL 키 | 첫 쪽에 조건이 없으면 NULL 행이 나온다 | 결정 11 |
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

시크릿, 호스트, 비밀번호는 담지 않는다. 읽을 때 `v`, `fp`, 키의 수가 다르거나 키 글이 4 KiB를 넘거나 JSON이 아니거나 토큰이 8 KiB를 넘으면 토큰이 아니고 처음부터 읽는다(오류가 아니다). 쓸 때 8 KiB를 넘으면 토큰을 싣지 않는다. 키 값은 SQL 글에 넣지 않고 매개변수로만 쓴다. 키가 NULL인 행의 쪽에는 토큰이 없다.

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
- **egress에 PostgreSQL 전용 연결 함수**: 커넥터의 `Dialer`가 이미 같은 일을 한다. 새 함수는 표면만 넓힌다.
- **`pq.NewConfig`와 `PG*` 변수가 있으면 시작하지 않기**(`adapter.pgconn`의 방식): 환경을 읽는 길을 막지 않고 알아서 피하는 쪽이 더 단단하다.
- **서버 쪽 커서(`DECLARE CURSOR`)나 `COPY`**: 트랜잭션을 읽기 내내 열어 둔다(원본의 vacuum을 막는다).
- **`OFFSET`으로 쪽 나누기**: 원본이 바뀌면 행이 빠지거나 겹치고, 뒤로 갈수록 느리다.
- **쪽 사이의 하나의 긴 스냅숏 트랜잭션**: 원본에 긴 트랜잭션을 거는 일이라 이 어댑터의 전제에 어긋난다. 쪽 사이에 원본이 바뀌면 각 쪽은 그 쪽이 본 상태이고 행은 겹치지 않는다(키가 오름차순이다).
- **문장을 첫 낱말로만 거르기**: 준비와 읽기 전용 트랜잭션이 더 믿을 만해서 거르개는 두지 않았다(REQ-490의 첫 낱말 거르개는 남는 부분).
- **테스트 서버에 TLS를 켜기**: 이 변경의 범위가 아니다(남는 위험).

## 남는 위험

- **TLS의 정상 경로는 이 환경에서 테스트하지 못했다.** 테스트 서버는 TLS를 켜지 않고 `crypto/tls`와 `net`은 이 유닛에서 막혀 있다. 원형에서 TLS를 앞에서 받는 Go 프록시로 맞는 CA, 틀린 CA, 이름 불일치를 확인했다. 테스트는 TLS 없는 서버에 `verify-full`이면 거절하고 평문으로 내려가지 않는 것만 덮는다. TLS를 켠 서버를 `with-postgres.sh`에 더하는 일은 이 변경에서 하지 않았다.
- **취소 요청의 연결에는 평문 표시가 없다.** 루프백이 아닌 평문 소스에서 취소가 거절되면 서버의 질의는 문장 시간까지 돈다.
- **사용자 `sql`은 읽기 전용 트랜잭션이 막지 못하는 함수를 부를 수 있다**(권고 잠금, `pg_notify`, `dblink`, 권한이 있는 계정의 `COPY … PROGRAM`, 시간 한도를 `set_config`로 푸는 일). 경계는 소스 쪽의 읽기 전용 계정이다. 문장은 정의를 편집할 권한이 있는 사람이 쓴다.
- **비밀번호의 사본이 힙에 남는다**(`Config`의 문자열). 지우지 못한다.
- **TLS 설정 등록소는 프로세스 전역이다.** 키를 읽기마다 새로 만들어 부딪히지 않고 닫을 때 지운다.
- **NULL 키를 세는 일은 전체 훑기다.** 큰 표에서는 읽기를 시작할 때 한 번 드는 비용이다.
- **키 글의 같음은 결정적 콜레이션을 가정한다.** 비결정적 콜레이션(대소문자를 같게 보는 것)이면 서로 다른 글이 한 키로 비교되어 행이 빠질 수 있다.
- **`trim_scale`은 PostgreSQL 13 이상이다.** 제품의 기준은 18이지만 더 낮은 소스의 `numeric` 키는 읽지 못한다.
- **표 이름은 쓴 그대로**(따옴표를 하므로 대소문자를 구분한다).
- **줄 번호와 쪽 번호는 이어 읽을 때 다시 1부터다.**
- **쪽 사이에는 원본이 바뀔 수 있다**(각 쪽은 한 시점이다). 수집의 한 번이 하나의 스냅숏이 되지는 않는다.
- **`MaxConns`가 1이라 쪽 읽기는 순차다.**

## 결과

- 새 유닛, `third_party`의 한 줄, 커버리지 하한의 새 키(도구로), 새 테스트의 잠금이 더해진다. 잠긴 테스트를 바꾸지 않는다.
- ADR-0091의 "뒤로 미룬 일" 가운데 PostgreSQL 연결 감싸개 함수는 **필요 없음**으로 닫힌다(결정 2).
- 조립부와 소스 탐색, 쓰기 권한 점검은 이어지는 변경이다.
