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
- 같은 주소가 연속으로 10번 거절당하면 5분 동안 429예요. 이 한도는 프로세스 메모리에 있어서 재시작하면 지워져요.
- 인증 저장소가 응답하지 못하면 401이 아니라 503이에요. 클라이언트는 토큰을 버리지 말고 다시 시도해요.
- `identity.json`(또는 `identity_record` 테이블)에 쓸 수 있는 사람은 전권을 가져요. 데이터 폴더의 소유권을 서버 계정으로 한정해요. 다른 사용자가 읽을 수 있는 `identity.json`은 서버가 열기를 거절해요.
- `praxium demo`와 `praxium local`은 이 절차와 상관없이 그대로예요.
