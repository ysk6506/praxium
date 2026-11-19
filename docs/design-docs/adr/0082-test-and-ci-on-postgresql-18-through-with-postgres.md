---
title: ADR-0082 테스트와 CI의 PostgreSQL을 18로 올리고 CI를 로컬과 같은 길로 돌린다
kind: decision
status: active
enforced_by:
  - none
no_enforcement_reason: 테스트와 CI를 돌리는 방식의 결정이다. 18 미만이면 scripts/with-postgres.sh가 실패하는 것은 스크립트 자체가 지키고, CI의 설치 단계는 18이 아니면 실패한다. 이 환경에서는 CI를 돌릴 수 없어 첫 풀 리퀘스트의 CI가 확인한다.
touches_protected:
  - .github/workflows/ci.yml
  - docs/design-docs/adr/0032-postgresql-adapter-closes-the-storage-port-and-td-0011.md
updates_docs:
  - README.md
  - docs/design-docs/harness-guide.md
amends:
  - adr: "0032"
    what: 결정 10의 테스트 서버 PostgreSQL 16과 CI의 서비스 컨테이너 `postgres:16`(러너에 깐 18의 서버 프로그램과 `scripts/with-postgres.sh`로 바뀐다)
amends_tests: []
decision_hash: c63de907b3f087af
---
# ADR-0082 테스트와 CI의 PostgreSQL을 18로 올리고 CI를 로컬과 같은 길로 돌린다

## 맥락

[ADR-0080](0080-redesign-storage-and-queries-on-one-postgresql.md)이 제품의 기준 버전을 PostgreSQL 18로 정했다. 그런데 테스트는 아직 16에서 돌고, CI는 서비스 컨테이너 `postgres:16`을 쓴다([ADR-0032](0032-postgresql-adapter-closes-the-storage-port-and-td-0011.md) 결정 10). 뒤의 변경은 테스트용 서버에 복제본이 되는 둘째 인스턴스를 더해야 하는데, 서비스 컨테이너로는 그것을 띄울 수 없다.

## 결정

1. 제품의 기준 버전은 18이고([ADR-0080](0080-redesign-storage-and-queries-on-one-postgresql.md) 결정 1) 테스트와 CI가 18에서 돈다.
2. CI는 서비스 컨테이너를 쓰지 않는다. 러너에 PostgreSQL 18의 서버 프로그램을 깔고(`scripts/ci-install-postgres.sh`) 검사를 `scripts/with-postgres.sh`로 감싸 돌린다. 로컬과 같은 길이다.
3. PostgreSQL이 필요한 테스트는 건너뛰지 않는다. 서버가 없으면 실패한다([ADR-0032](0032-postgresql-adapter-closes-the-storage-port-and-td-0011.md) 결정 10 그대로).
4. `scripts/with-postgres.sh`는 찾은 서버 프로그램이 18 미만이면 이유를 알리고 실패한다.

## 이유

- 테스트용 서버의 설정이 한 곳(`scripts/with-postgres.sh`)에만 있다. 서비스 컨테이너를 두면 로컬과 CI에 설정이 둘이 되어 어긋난다.
- 뒤의 변경이 같은 스크립트에 둘째 인스턴스를 더하면 CI도 그대로 받는다.
- 개발자가 로컬에서 돌리는 것과 CI가 돌리는 것이 같으면 "CI에서만 실패한다"가 줄어든다.

## 대안과 기각 사유

| 대안 | 기각 사유 |
|---|---|
| 서비스 컨테이너의 태그만 18로 | 둘째 인스턴스를 띄울 수 없고 설정이 두 곳에 남는다 |
| CI에서 컨테이너 둘을 손으로 띄우기 | 로컬과 길이 달라진다 |

## 결과

- 로컬에서는 18의 서버 프로그램이 있는 폴더를 `PG_BIN`으로 준다. 예: `PG_BIN=/usr/lib/postgresql/18/bin scripts/with-postgres.sh pnpm check`.
- CI의 `test` 잡은 PostgreSQL 공식 apt 저장소에서 `postgresql-18`을 깔고 버전이 18인지 확인한다. 꾸러미가 띄우는 기본 클러스터는 쓰지 않으므로 멈춘다.
- 서버가 18 미만이면 시작을 거절하는 버전 관문은 이 결정에 없다. 다음 변경이 한다.
- 제품 코드는 바뀌지 않는다.
