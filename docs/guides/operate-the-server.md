---
title: 서버 운영하기 — 첫 관리자, 사용자와 토큰, 운영 모드의 시작 조건
kind: guide
status: active
follows:
  - docs/design-docs/adr/0044-built-in-authentication-with-access-tokens.md
covers:
  - backend/cmd/praxium/admin.go
  - backend/cmd/praxium/auth.go
---
# 서버 운영하기

`praxium api serve`를 운영 모드로 띄우고 사용자와 접근 토큰을 관리하는 순서예요. 왜 이렇게 정했는지는 ADR-0044에 있어요. 비밀번호와 기본 계정은 없어요. 신원은 접근 토큰 하나예요.

## 1. 처음 한 번: 첫 관리자

서버 기계에서 서버를 띄우기 전에 실행해요. 데이터 폴더(`-store file`)는 서버가 돌고 있지 않을 때만 열려요.

```
praxium admin bootstrap -name root -store file -store-data-dir /var/lib/praxium
```

토큰이 표준 출력에 한 줄로 한 번만 나오고 안내와 감사 로그는 표준 오류로 나가요. 토큰은 바로 비밀 저장소나 파일(권한 0600)에 옮겨요. 다시 볼 수 없어요. 활성 관리자가 이미 있으면 거절해요. PostgreSQL은 `-store postgres -store-dsn-file <파일>`이에요.

## 2. 서버 띄우기

```
praxium api serve -addr 127.0.0.1:8080 -store file -store-data-dir /var/lib/praxium
```

운영 모드(기본값)는 아래가 하나라도 어긋나면 스토어를 열기 전에 시작을 거부하고 무엇을 하면 되는지 말해요.

| 거부하는 경우 | 고치는 법 |
|---|---|
| 스토리지가 `memory`(기본값) | `-store file -store-data-dir`나 `-store postgres -store-dsn-file` |
| `-dev-*` 플래그나 개발용 토큰 환경 변수 | 지운다. 개발용은 `-mode development`에서만 쓴다 |
| 활성 관리자가 없다 | 1절의 `admin bootstrap` |
| 루프백이 아닌 주소에 TLS도 `-behind-proxy`도 없다 | `-tls-cert`와 `-tls-key`, 또는 TLS를 끝내는 프록시 뒤에서 `-behind-proxy` |
| `-tls-cert`와 `-tls-key` 중 하나만 있다 | 둘 다 준다 |
| `-store postgres`인데 접속 문자열의 호스트가 다른 기계이고 `sslmode`가 `verify-full`이나 `verify-ca`가 아니다 | 5절 |

토큰은 평문 HTTP로 보내면 안 돼요. `-behind-proxy`는 프록시가 TLS를 끝내고 `X-Forwarded-For`를 덧붙인다는 운영자의 선언이에요. 서버는 그 헤더의 마지막 값을 호출자의 주소로 믿어요. 프록시와 서버 사이는 안전한 망이어야 해요.

## 3. 사용자와 토큰

관리자 토큰을 환경 변수로 주고 명령을 실행해요(`--token`은 프로세스 목록에 보여서 권하지 않아요).

```
export PRAXIUM_ENDPOINT=https://praxium.example.org
export PRAXIUM_TOKEN=<관리자 토큰>
praxium admin user create ada --role viewer --display-name "Ada Example"
praxium admin token create --owner ada --role viewer --expires-in-days 30 --description "dashboard"
praxium whoami
```

- 토큰은 만들 때 한 번만 보여요. 서버는 해시만 가져요.
- 모든 토큰은 만료해요. 기본 90일이고 1일~365일(API는 1시간부터)이에요.
- 서비스 계정(수집기 같은 프로그램)은 `praxium admin account create`예요. `admin`은 가질 수 없고 `ingester`는 서비스 계정만 가져요.
- 토큰이 가진 역할은 토큰의 역할과 소유자의 현재 역할의 교집합이에요.
- 폐기는 `praxium admin token revoke <토큰 ID>`이고(ID는 `token list`에 있어요) 다음 요청부터 거부해요. 사용자를 비활성화하면 그 사용자의 모든 토큰이 거부되고 되돌릴 수 없어요. 마지막 활성 관리자는 비활성화할 수 없어요.

## 4. 토큰을 잃었을 때

모든 관리자의 토큰을 잃거나 만료됐으면 서버 기계에서 기존 사용자의 새 토큰을 받아요. `bootstrap`은 관리자가 있으면 거절해요.

```
praxium admin issue-token -owner root -store file -store-data-dir /var/lib/praxium
```

감사 로그에 로컬 운영자가 만든 것으로 남아요.

## 5. 지켜볼 것

- 로그에서 `audit: true`인 줄이 감사 로그이에요: `bootstrap`, `principal_created`, `token_issued`, `token_revoked`, `auth_failed`, `auth_blocked`. 토큰의 값과 해시는 어디에도 없어요.
- 같은 주소에서 거절된 토큰이 10번 쌓이면 5분 동안 그 주소의 거절된 토큰에 429를 줘요. 유효한 토큰은 막히지 않아서 프록시 뒤의 다른 사람에게 영향이 없어요. 이 기록은 프로세스 메모리에 있어서 재시작하면 지워져요.
- 인증 저장소가 응답하지 못하면 401이 아니라 503이에요. 클라이언트는 토큰을 버리지 말고 다시 시도해요.
- `identity.json`(또는 `identity_record` 테이블)에 쓸 수 있는 사람은 전권을 가져요. 데이터 폴더의 소유권을 서버 계정으로 한정해요. 다른 사용자가 읽을 수 있는 `identity.json`은 서버가 열기를 거절해요.
- `praxium demo`와 `praxium local`은 이 절차와 상관없이 그대로예요.

## 5. PostgreSQL 접속의 TLS

운영 모드에서 데이터베이스가 다른 기계에 있으면 서버 인증서를 확인하는 접속이어야 시작해요(ADR-0064). 접속 문자열(`-store-dsn-file`의 파일)에 `sslmode=verify-full`이나 `sslmode=verify-ca`를 적어요. 인증서를 낸 CA를 이 기계가 믿지 않으면 `sslrootcert=<파일>`도 줘요. `sslmode`를 적지 않으면 드라이버는 `require`로 접속하는데, 이것은 인증서를 확인하지 않아서 거부해요. 호스트가 여럿이면 하나라도 다른 기계일 때 이 규칙을 적용해요.

- 같은 기계의 데이터베이스(`127.0.0.1`, `::1`, `localhost`, 유닛스 소켓)는 확인하지 않아요.
- 사설망이나 터널 같은 다른 수단으로 접속을 보호한다면 `-allow-unverified-db-tls`로 시작할 수 있어요. 시작 로그에 경고가 남아요.
- 개발 모드와 `-store file`에는 영향이 없어요.

## 6. 에이전트용 MCP 인터페이스(읽기 전용)

`POST /mcp`는 KG를 읽는 MCP 인터페이스예요(ADR-0062, ADR-0068). 운영 모드에서는 기본으로 꺼져 있어서 `-mcp`를 줘야 켜져요. 켜면 서버가 같은 주소와 포트에서 `/mcp`를 내요.

```
praxium api serve -addr 127.0.0.1:8080 -store file -store-data-dir /var/lib/praxium -mcp
```

- **받는 토큰**: 토큰이 가진 역할이 `viewer` 하나뿐인 서비스 계정의 토큰만 받아요(`praxium admin account create`로 만들고 `viewer`만 주거나, 다른 역할이 있는 계정이면 `viewer`로 좁혀 토큰을 발급해요). 사람의 토큰이나 `viewer` 말고 다른 역할이 있는 토큰은 HTTP 403 `permission.mcp_not_allowed`이고 본문에 이유가 있어요. 개발 모드와 `demo`, `local`의 실행용 토큰은 받아요.
- **브라우저**: 브라우저가 아닌 클라이언트는 `Origin`을 보내지 않아서 그대로 받아요. 브라우저에서 부르려면 `-mcp-allowed-origins https://console.example`처럼 출처를 적어요(쉼표로 여럿). 목록에 없는 `Origin`은 403이에요. 서버를 루프백 주소에 묶었고 `-behind-proxy`를 주지 않았으면 `Host`도 루프백 이름(`localhost`, `127.0.0.1`, `::1`)만 받아요. 프록시 뒤(`-behind-proxy`)에서는 프록시가 바깥 이름을 `Host`로 넘기므로 `Host`는 보지 않고 `Origin`과 토큰만 봐요.
- **꺼져 있을 때**: `/mcp`와 `/.well-known/` 아래는 JSON 404이고 웹 화면이 대신 나오지 않아요.
- **지켜볼 것**: 툴을 부를 때마다 로그에 `read_record: true`인 `mcp_read` 줄이 한 줄 남아요(토큰 ID, 추적 ID, 툴, 판, 클라이언트 이름, 걸린 시간). 판별 요청 수와 판 헤더 없이 거절한 수는 `kg_whoami`의 `server.requests_by_version`에서 봐요(프로세스가 뜬 뒤의 숫자예요).
