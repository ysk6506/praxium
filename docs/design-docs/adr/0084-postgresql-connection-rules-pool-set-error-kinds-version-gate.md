---
title: ADR-0084 PostgreSQL 연결의 규칙 — 풀 셋, 오류의 분류, 버전 관문
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH020
  - GOCOV001
  - TLOCK001
  - backend/internal/pgconn/classify_test.go
  - backend/internal/pgconn/rules_test.go
  - backend/internal/pgconn/limiter_test.go
  - backend/internal/pgstore/outcome_test.go
  - backend/internal/pgstore/session_rules_test.go
  - backend/cmd/praxium/poolflags_test.go
touches_protected:
  - docs/design-docs/adr/0032-postgresql-adapter-closes-the-storage-port-and-td-0011.md
  - docs/product-specs/requirements-operations.md
  - docs/product-specs/requirements-access.md
  - harness/config/coverage-floor.json
  - harness/config/test-lock.json
updates_docs:
  - docs/design-docs/postgres-ha.md
  - docs/design-docs/postgres-operations.md
  - docs/guides/operate-the-server.md
  - docs/product-specs/requirements-operations.md
  - docs/product-specs/requirements-access.md
amends:
  - adr: "0032"
    what: 결정 2의 연결 하나(`sql.OpenDB` 풀 하나, 연결 문자열 하나)와 한계 (2)의 연결 풀 크기 가정(풀 하나에 16)
amends_tests:
  - go:backend/internal/pgconn::TestConfigFillsDefaultsAndRefusesWhatIsWrong
test_change_reason: 풀을 하나(16)에서 셋(쓰기 8, 읽기 8, 임의 질의 4)으로 나누는 것이 요구사항(REQ-554)이다. 그래서 기본값 단언 16이 8로 바뀌고, 읽기와 임의 질의 풀의 크기, 연결 시한 5초, 초당 새 연결 20의 단언이 더해진다. 빠지는 단언은 없고 나머지 단언은 그대로다.
decision_hash: da3f8d2e0517998c
---
# ADR-0084 PostgreSQL 연결의 규칙 — 풀 셋, 오류의 분류, 버전 관문

## 맥락

ADR-0081이 PostgreSQL이 고장 났을 때 제품이 지킬 규칙을 정했고, ADR-0083이 연결 기반 `adapter.pgconn`을 나눴다. 이 결정은 그 규칙 가운데 **PostgreSQL 인스턴스 하나로 확인할 수 있는 것**을 구현한다: 풀 셋, 오류의 분류와 풀 버리기, "커밋됐는지 모름", 새 연결의 속도 제한, 버전 관문. 둘째 인스턴스(복제본)와 승격의 테스트, 죽은 연결의 감지(`Dialer`, keepalive, `TCP_USER_TIMEOUT`)는 다음 변경이 한다. 드라이버(`lib/pq` v1.12.3)의 동작은 소스를 읽어 확인했다.

## 결정

1. **버전 관문.** `pgconn.Open`은 연결한 서버의 `server_version_num`을 읽어 180000 미만이면 오류 코드 `pgconn_server_too_old`와 이유를 알리고 시작하지 않는다(마이그레이션 전에). 읽기용 연결 문자열이 따로 있으면 그 서버도 본다. PostgreSQL에 붙는 모든 명령이 `pgconn.Open`을 지나므로 모두 같다. 버전을 읽는 함수는 `Config.ReadVersion`으로 바꿀 수 있어 17과 18의 경우를 테스트한다.
2. **풀 셋.** 쓰기 풀(기본 8, 설정 `MaxOpenConns`), 읽기 풀(8, `ReadConns`), 임의 질의 풀(4, `QueryConns`. 쓰는 코드는 질의 실행기의 변경에 온다). `MaxIdleConns`(4)는 풀마다의 상한이고 `ConnMaxLifetime`(30분)은 지금 값 그대로다. 읽기 전용 트랜잭션은 읽기 풀, 그 밖의 모든 쓰기는 쓰기 풀이다. 마이그레이션과 스키마 변경은 쓰기 풀에서 마이그레이션 역할의 시한으로 돈다. 인증 기록의 읽기는 읽기 풀, 쓰기는 쓰기 풀이다. 쓰기 풀의 크기 설정은 이름을 `MaxOpenConns`로 두었다(설정 이름을 바꾸면 `pgstore`의 잠긴 테스트가 바뀐다).
3. **연결 문자열.** 둘을 받는다: 쓰기용(`-store-dsn-file`)과 읽기용(`-store-read-dsn-file`. 없으면 쓰기용). 여러 호스트를 받는다. 쓰기 풀의 `target_session_attrs=read-write`는 글자가 아니라 **코드에서** `pq.NewConfig`로 읽은 뒤 넣고, 쓰기용 문자열이 다른 값을 적으면 거절한다. `connect_timeout`이 없거나 0이면 기본 5초를 넣는다(드라이버는 무한정 기다린다). 연결 문자열의 모르는 키는 드라이버가 서버의 시작 파라미터로 보내므로 거절한다(키 이름만 알리고 값은 알리지 않는다). 드라이버는 `PG*` 환경 변수를 항상 읽고(`pq.NewConfig`는 `os.Environ()`을 쓰며 끄는 길이 없다) 연결 문자열에 없는 값을 채우므로, 그런 변수가 하나라도 있으면 이유를 알리고 시작하지 않는다(변수 목록은 `pq.Config`의 `env` 태그에서 읽는다). 운영 모드의 TLS 확인은 읽기용 문자열에도 적용한다(더 약한 쪽으로 판단).
4. **오류의 분류**(`pgconn.Classify`). (1) 문맥의 취소와 시한은 호출자의 것. (2) 서버가 답한 오류(`*pq.Error`)는 서버의 것이고, SQLSTATE 25006, 57P03, 클래스 08이면 "역할이 바뀜"(57P03은 접속 중에 FATAL로 오므로 심각도보다 코드를 먼저 본다). (3) `driver.ErrBadConn`과 FATAL인 그 밖의 오류는 "연결을 잃음". (4) 그 밖의 드라이버 오류는 모두 "연결을 잃음"(`net`의 형 이름을 쓰지 않는다). 트랜잭션 안의 작업이 돌려준 오류는 드라이버의 것으로 보이는 것(서버의 답, `ErrBadConn`, 파일 끝, `Timeout()`을 가진 오류)만 분류한다. 호출자 자신의 오류(행 없음 등)는 풀에 상관없다. 읽기 전용으로 연 트랜잭션의 25006은 호출자의 잘못이지 서버의 역할 변화가 아니므로 풀을 버리지 않는다. 다른 PostgreSQL 어댑터가 쓸 `IsUniqueViolation`도 같은 곳에 있다.
5. **풀 버리기.** "역할이 바뀜"이나 "연결을 잃음"을 만나면 그 풀의 `*sql.DB`를 새로 만들어 바꿔 달고 옛것을 닫는다. 세대 번호로 같은 실패를 동시에 본 호출자들이 한 번만 바꾼다. 진행 중인 트랜잭션은 옛 풀에서 끝난다(연결은 돌려줄 때 닫힌다). 호출자는 오류 코드 `pgconn_role_changed`나 `pgconn_connection_lost`를 받고 다시 시도할 수 있다.
6. **"커밋됐는지 모름".** `Commit`이 `nil`이면 커밋됨, FATAL이 아닌 `*pq.Error`(와 `ErrInFailedTransaction`)면 서버가 답한 실패, 문맥이 끝나 보내기 전에 돌아왔으면 커밋 안 됨, **그 밖은 모두 "모름"**: 오류 코드 `pgconn_outcome_unknown`을 돌려주고 풀을 버린다. 성공으로도 실패로도 답하지 않는다. `pgstore`의 머리를 옮기는 쓰기(`ApplyChunk`, `AbandonApply`, `Seal`, `Restore`)는 이 코드를 받으면 쓰기 풀(주 서버)에서 머리 행을 다시 읽는다: 머리가 그 쓰기가 정한 값이면 커밋된 것으로 오류를 없애고, 아니면 커밋 안 된 것으로 "다시 시도하라"는 오류를 돌려준다(적용자는 지금처럼 머리와 `ApplyID`로 되풀이를 가른다). 머리도 못 읽으면 "모름"이 그대로다. 그 밖의 쓰기(리스, 카탈로그, 인증 기록)는 "모름"을 그대로 돌려준다.
7. **쓰기는 반드시 트랜잭션 도우미로, 세션에 남는 것은 쓰지 않는다.** `pgconn`과 `pgstore`는 자기 비테스트 소스를 `//go:embed`로 담아(구조 계약이 유닛 밖의 파일 읽기를 막으므로) 테스트가 훑는다. 규칙의 구현은 `pgconn.SessionStateViolations` 하나이고 두 유닛의 테스트가 부른다. 금지: 트랜잭션이 아닌 것에 `Exec`, `Prepare`, `LISTEN`, `NOTIFY`, `CREATE TEMP`, `LOCAL`이 아닌 `SET`, `set_config(…, false)`, `pg_advisory_lock(`. 지금 소스는 걸리지 않는다.
8. **새 연결의 속도 제한.** 풀 셋이 한 제한기를 나눠 쓰고, `driver.Connector`를 감싸 초당 새 연결 수를 제한한다(기본 20, `NewConnsPerSecond`. 작은 몫의 연속 여유가 있는 셀 속도 알고리즘이고 시계는 `kernel.Clock`이라 테스트는 가짜 시계로 돈다).
9. **조립부.** `api serve`와 `admin`의 명령이 `-store-read-dsn-file`, `-store-write-conns`, `-store-read-conns`, `-store-query-conns`, `-store-new-conns-per-second`를 받는다(0은 기본값).

## 이유

- 풀을 역할로 나눠 두면 읽기 풀을 복제본으로 보내는 일이 뒤의 단계에서 설정만으로 된다.
- 드라이버는 치명적 오류를 `ErrBadConn`으로 바꾸고 25006은 연결을 풀에 남긴다. 제품이 분류하고 풀을 버려야 장애 조치 뒤 쓰기가 새 주 서버로 돌아온다.
- 보내기 전에 끝난 것과 보낸 뒤 끊긴 것이 밖에서는 같은 값이라 "모름"을 보수적으로 가르고, 머리 행이 답을 안다.

## 대안과 기각 사유

- **`PG*` 환경 변수를 지우고 시작한다.** 지우려면 `os`가 필요한데 `pgconn`은 못 쓰고, 조립부가 지우면 라이브러리로 쓰는 쪽이 보호받지 못한다. 거절이 더 단순하고 확실하다.
- **소스 훑기를 한 유닛의 테스트가 다른 유닛의 파일을 읽어서 한다.** `os`와 파일 읽기가 막혀 있다. 유닛마다 자기 소스를 담는다.
- **`SetMaxIdleConns(0)`으로 유휴 연결만 닫는다.** 진행 중인 연결은 남는다. 새 풀로 바꾸는 쪽이 모두 버린다.
- **설정 이름을 `WriteConns`로 바꾼다.** `pgstore`의 잠긴 테스트가 `MaxOpenConns`를 단언한다. 이름은 두고 뜻만 "쓰기 풀"로 적는다.

## 결과

- 잠긴 테스트 하나(`go:backend/internal/pgconn::TestConfigFillsDefaultsAndRefusesWhatIsWrong`)의 기본값 단언이 바뀐다: 16 → 8, 단언 추가(`ReadConns` 8, `QueryConns` 4, 연결 시한 5초, 초당 20).
- 연결 시한(`context`)과 죽은 연결의 감지, 둘째 인스턴스를 띄운 승격 테스트, `Dialer`는 이 결정에 없고 다음 변경이다. 읽기 풀이 복제본으로 가면, 쓰기 앞의 읽기(인증 기록의 `Get`, 리스의 읽기)는 주 서버를 읽도록 바꿔야 한다.
- `pgconn.Handle`은 `*sql.DB`를 한 번 감싼 것이라 풀이 바뀌어도 쓰는 쪽이 새 풀을 본다.
