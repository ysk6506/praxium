---
title: ADR-0064 보안의 기본 셋 — 취약점 신고 창구, 워크플로의 액션 고정, 운영 모드의 PostgreSQL 접속 TLS
kind: decision
status: active
enforced_by:
  - WFL001
  - harness/src/checks/security-policy.test.ts
  - backend/cmd/praxium/dbtls_test.go
updates_docs:
  - README.md
  - docs/design-docs/harness-guide.md
  - docs/guides/operate-the-server.md
  - docs/guides/release.md
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements-web-cli-release.md
touches_protected:
  - harness/**
  - .github/**
  - docs/design-docs/adr/**
  - docs/product-specs/requirements-*.md
decision_hash: 8a959b199628fb16
---
# ADR-0064 보안의 기본 셋

## 맥락

프락시움은 앞으로 에이전트의 툴 호출을 통제하는 게이트웨이까지 맡는다. 안전을 맡는 제품은 자기 공급망과 저장소 접속이 먼저 믿을 만해야 한다. 보안 조사에서 지금 바로, 적은 비용으로, 효과가 큰 것 셋이 나왔다. 이 결정은 그 셋만 다룬다.

## 결정

1. **취약점 신고 창구 (REQ-470).** 루트에 `SECURITY.md`를 둔다. 취약점은 공개 이슈가 아니라 GitHub의 비공개 취약점 신고로 받는다. 연락 수단은 그 하나이고 사람의 이메일 주소는 쓰지 않는다. 신고 항목, 지원하는 버전(알파이므로 가장 최근 릴리스와 main), 받은 뒤의 흐름을 적고 처리 기한은 약속하지 않는다. 비공개 신고 기능을 켜는 것은 저장소 설정이고 이 변경의 일이 아니다.
2. **워크플로의 액션을 커밋 해시로 고정 (REQ-471).** 바깥 액션은 `uses: owner/name@<40자리 해시> # 버전`으로만 참조하고, 하네스 검사 `WFL001`이 태그, 브랜치, 버전 없음을 막는다. 같은 리포지토리의 액션과 다이제스트로 고정한 컨테이너 이미지는 통과한다. 지금 쓰는 주 버전 태그가 가리키는 커밋으로 고정해 동작은 바뀌지 않는다. 올리는 일은 사람이 해시를 바꾸는 풀 리퀘스트로 하고(절차는 릴리스 안내 8절), 자동 갱신 도구는 넣지 않는다. `.github/`는 보호 경로라 변경에 결정 기록이 필요하고, 자동 풀 리퀘스트는 이 검사를 통과하지 못한다.
3. **운영 모드에서 PostgreSQL 접속의 TLS 확인 (REQ-472, REQ-473).** 운영 모드에서 스토리지가 PostgreSQL이고 접속 대상 가운데 하나라도 같은 기계(루프백 주소, `localhost`, 유닛스 소켓)가 아니면, `sslmode`가 `verify-full`이나 `verify-ca`여야 스토어를 열기 전에 시작한다. 판정은 스토어가 쓰는 드라이버의 파서(`pgstore.InspectTLS`)로 하고, `sslmode`가 없으면 드라이버의 기본값 `require`로 본다. 운영자가 새 플래그 `-allow-unverified-db-tls`로 허용하면 시작하되 시작 로그에 경고를 남긴다. 개발 모드와 파일 스토어에는 영향이 없다.

## 이유

- 취약점을 비공개로 받을 창구가 없으면 신고자가 공개 이슈에 올리게 된다. 창구는 문서 하나와 저장소 기능 하나로 된다.
- 태그는 나중에 다른 코드로 옮겨질 수 있다. 워크플로는 릴리스 파일을 만들고 시크릿 가까이에서 돈다. 해시는 옮겨지지 않는다.
- 인증서를 확인하지 않는 TLS는 암호화만 하고 상대가 맞는지는 모른다. 다른 기계의 데이터베이스에는 데이터와 접속 정보가 오간다. 기본값 `require`가 이를 막지 못하므로 시작 조건으로 삼는다(운영 모드의 다른 시작 조건과 같은 방식이다, ADR-0044).

## 대안과 기각 사유

- 보안 이메일 주소를 적는 안: 사람의 주소를 공개 문서에 남기고 받는 쪽의 관리가 따로 필요해서 기각했다.
- 해시 고정에 자동 갱신 도구를 넣는 안: 그 도구의 변경은 보호 경로를 바꾸므로 결정 기록이 없어 검사를 통과하지 못한다. 기각했다.
- 접속 문자열을 새로 파싱하는 안: 드라이버가 읽는 것과 어긋날 수 있다. 같은 파서를 써서 기각했다.
- 원격이면 무조건 거부하고 허용 플래그를 두지 않는 안: 사설망이나 터널로 보호하는 구성이 있어 명시적 허용만 두었다.

## 하지 않은 것

- 릴리스 파일의 서명, SBOM, 빌드 출처 증명은 오픈 베타 전에 한다(ADR-0039).
- 액션의 자동 갱신 도구는 넣지 않는다.
- `admin bootstrap` 같은 다른 명령의 접속에는 이 시작 조건을 적용하지 않았다. 서버 시작만 다룬다.

## 결과

`SECURITY.md`와 `WFL001`, `-allow-unverified-db-tls`가 생긴다. 이미 원격 PostgreSQL에 `require`로 붙어 운영하던 설치는 다음 시작부터 `sslmode`를 고치거나 플래그를 줘야 한다.
