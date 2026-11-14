---
title: ADR-0041 명령줄 사용성, 로컬 모드, 수집 결과와 수집 상태를 데이터 폴더에 남기기
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH011
  - GOARCH052
  - GOCOV001
  - TLOCK001
  - TRACE006
  - backend/cmd/praxium/help_test.go
  - backend/cmd/praxium/ingestkeep_test.go
  - backend/cmd/praxium/ingestinit_test.go
  - backend/cmd/praxium/local_test.go
  - backend/internal/ingest/statefile_test.go
  - backend/internal/cli/cause_test.go
  - backend/internal/httpapi/errordetail_test.go
updates_docs:
  - AGENTS.md
  - docs/design-docs/ingestion.md
  - docs/design-docs/rest-api.md
  - docs/design-docs/temporary-console.md
  - docs/guides/release.md
  - docs/product-specs/requirements.md
touches_protected:
  - AGENTS.md
  - docs/product-specs/requirements.md
  - harness/config/test-lock.json
  - docs/design-docs/adr/0033-ingestion-vertical-slice-from-a-file-source.md
  - docs/design-docs/adr/0040-excel-workbook-source-read-with-the-standard-library.md
amends:
  - adr: "0033"
    what: ingest run의 스토리지가 명령이 도는 동안만 있다는 결과
  - adr: "0040"
    what: 테스트용 워크북 작성기를 제품이 부르지 않는다는 문장
decision_hash: d795851d2e5b9aff
---
# ADR-0041 명령줄 사용성, 로컬 모드, 수집 결과와 수집 상태를 데이터 폴더에 남기기

## 맥락

릴리스 실행 파일(ADR-0039)을 처음 쓰는 사람의 입장에서 독립 검증을 했고 "처음 쓰는 사람에게 주기에는 아직 이르다"는 판정이 나왔다. 명령줄과 백엔드 쪽의 결함은 다섯 가지로 묶인다.

1. **도움말이 없다.** `praxium --help`는 오류이고, 최상위 사용법에는 `kg`, `query`, `types`, `ingest`가 없으며, `api serve -h`는 플래그 목록 없이 `flag: help requested`만 낸다. 모르는 플래그를 주면 12줄짜리 사용법이 쏟아진다. 하는 일이 없는 역할(`worker`, `collector`)이 사용법에 있고 실행하면 시작했다고 로그하고 끝난다.
2. **오류에 원인이 없다.** 서버는 `params`와 `errors`로 무엇이 틀렸는지 알려 줄 수 있지만 명령줄은 `invalid_input.field (HTTP 422)`만 보여 준다. 404는 무엇을 못 찾았는지 말하지 않는다. 운영 모드 오류는 내부 부채 번호를 말한다.
3. **수집 결과를 볼 수 없다.** `ingest run`의 스토리지와 수집 상태(원본 레코드의 구간, 안 보인 횟수, 식별 색인)는 명령이 끝나면 사라진다. 그래서 자기 파일을 넣어 웹 UI로 볼 방법이 없고, "사라진 행은 연속 3번 보이지 않고 24시간이 지나야 퇴역한다"는 규칙(REQ-267)은 한 프로세스 안에서만 성립한다.
4. **수집 명령의 종료 코드와 출력.** 문제가 있어도 항상 0이고, 소스에 없는 데이터셋 이름의 `-input`은 조용히 무시된다. `volatile`, `graph_hash`, `raw_added` 같은 항목은 설명이 없다.
5. **릴리스 사용자가 따라 할 수 있는 샘플이 없다.** 문서의 예시는 소스 체크아웃의 샘플 경로를 쓴다.

## 결정

**1. 도움말 (REQ-289).** `praxium`, `praxium help`, `praxium --help`, `praxium -h`는 같은 최상위 도움말을 낸다(맨 위에 가장 빠른 시작, 모든 명령을 묶음별로 한 줄 설명과 함께, 전역 플래그의 위치). 모든 명령과 하위 명령은 `-h`, `--help`, `praxium help <명령>`으로 사용법 한 줄, 설명, 플래그와 기본값, 예시를 낸다. 플래그 목록은 플래그 집합에서 만들어 기본값이 코드와 갈라지지 않는다(`cmd/praxium/help.go`, 서버에 묻는 명령은 `internal/cli/help.go`). 잘못된 명령줄은 오류 한 줄, 사용법 한 줄, "For details: praxium <명령> --help" 한 줄만 낸다. 역할 `worker`와 `collector`는 최상위 도움말에서 "아직 구현되지 않음"으로 밝히고 실행하면 `not implemented yet`과 종료 코드 1로 끝난다(`run`의 기존 동작과 그 테스트는 그대로 두고 `execute`에서 가로챈다). 전역 플래그(`--endpoint`, `--token`, `--config`, `--json`)는 명령 앞과 뒤 어디에 놓아도 받는다.

**2. 오류에 원인을 (REQ-290).** 오류 형식(RFC 9457)의 구조는 그대로 두고 이미 열려 있는 `params`에 키를 채운다. 계약의 `Problem.params` 설명문에 적는 것 외에 스키마가 바뀌지 않으므로 호환성을 깨지 않는다. `not_found.resource`는 `resource`와 함께 `id`(요청이 가리킨 KG 이름, 객체 ID, 타입 이름, 경로)를 싣는다. 질문과 목록의 값 오류(`invalid_input.field`)는 기존 `detail`에 더해 사람이 읽는 `message`와, 아는 경우 `field`(요청의 필드 이름)를 싣는다. `field`는 질의 유닛의 입력 오류가 필드 이름을 가진 오류(`query.FieldOf`)로 알린다. 명령줄은 이것을 한 줄로 보여 준다: `invalid_input.field: field "object_type" (--type): no object type "X" (HTTP 422, trace …)`. 이 값이 없는 응답(옛 서버)은 `detail`에서 코드를 뺀 문장을, 그것도 없으면 코드만 보여 준다. 운영 모드 오류는 내부 부채 번호를 사용자 메시지에서 빼고 `praxium demo`, `praxium local`, `-mode development` 중 무엇을 하면 되는지 말한다(`-mode production`을 직접 줬을 때만 잠긴 테스트가 요구하는 세부 문장이 괄호로 뒤따른다). `demo`와 `local`은 `-addr`의 형식 오류를 가장 먼저 말한다.

**3. 수집 결과와 수집 상태를 폴더에 남긴다 (REQ-291).** `ingest run -data-dir <폴더>`는 내장 파일 스토어(ADR-0034)의 그 폴더에 KG를 만들고(이름과 별칭은 `main`) 이어 쓴다. 수집 상태는 같은 폴더의 `ingest-state.json`이다.

| 항목 | 내용 |
|---|---|
| 형식 | `{"format": "praxium-ingest-state", "version": 1, "kg_id": "...", "sources": {"<소스 ID>": <State>}}`. `State`는 지금 메모리에 두는 것(실행 번호, 데이터셋별 원본 레코드의 열린 구간과 닫힌 구간, 안 보인 횟수, 식별 색인, 객체, 링크, 키) 그대로이다 |
| 쓰기 | 원자적이다. 임시 파일에 쓰고 `fsync`한 뒤 이름을 바꾸고 폴더를 `fsync`한다. 쓰기가 실패하면 옛 파일이 그대로이고 메모리의 상태도 되돌린다 |
| 버전 | 모르는 형식과 새 버전의 파일은 덮어쓰지 않고 거절한다 |
| 구조 | `core.ingest`의 `FileStateStore`가 `StateDisk`(읽기, 만들기, 이름 바꾸기, 삭제, 폴더 동기화)를 주입받는다. 운영체제는 `cmd.praxium`만 안다(`filedisk.go`의 `stateDisk`가 파일 스토어와 같은 폴더를 같은 잠금 아래에서 쓴다) |

KG와 상태가 어긋난 폴더는 아무것도 바꾸기 전에 거절한다. 상태 없이 KG만 있는 폴더(데모의 폴더이거나 상태 파일이 지워졌다)는 객체를 두 번 만들 수 있고, KG 없이 상태만 있으면 없는 객체를 퇴역시키려 하며, 다른 KG의 상태는 엉뚱한 객체를 가리킨다. 새 폴더는 KG를 만든 직후 상태 파일에 KG ID를 묶는다. 다른 프로세스(서버, 다른 수집)가 연 폴더는 파일 스토어의 잠금 오류에 "그 프로세스를 멈추고 다시 실행하라"를 더해 전한다. `-data-dir` 없이는 이전처럼 명령이 도는 동안만 있는 스토어이고 그 사실과 방법을 마지막에 알린다.

**4. 수집 명령의 검사와 출력 (REQ-292).** 소스 정의에 없는 데이터셋 이름의 `-input`은 종료 코드 2의 오류이고 정의에 있는 이름을 알린다. 정의에 있는데 `-input`이 없는 데이터셋은 설계의 완전성 규칙대로 이번에 수집하지 않은 것(불완전 수집과 다르게, 보이지 않음으로 세지 않고 원본 레코드와 객체를 그대로 둔다)이고 실행 전에 그것을 알린다. 문제를 따로 둔 실행은 기본이 종료 코드 0이고, 마지막 줄이 `completed with N problems (see "problem" lines above)` 또는 `completed without problems`이다. `-fail-on-problem`이면 문제가 있을 때 종료 코드 20이다(클라이언트 명령의 3~12와 겹치지 않는다). `--help`가 출력의 항목을 한 줄씩 설명하고 종료 코드 표를 싣는다. 기존 출력의 줄은 그대로 두고 줄을 더하는 방식이라 출력을 단언하는 테스트가 깨지지 않는다.

**5. `praxium ingest init` (REQ-293).** 실행 파일에 담긴 예제를 폴더에 쓴다. CSV와 JSON Lines의 소스 정의, 매핑 정의, 샘플 파일 다섯 개는 수집 합격 테스트의 샘플(`ingestcheck/testdata`)이고, 엑셀의 소스 정의와 매핑 정의는 `ingestcheck/samples-xlsx`다. 같은 파일이 합격 테스트와 사용자 예제의 두 곳에 있지 않도록 `ingestcheck`가 `embed`로 내보낸다(`ExampleFiles`). 샘플 워크북은 xlsx 유닛의 작성기로 그 자리에서 만든다(이진 파일을 커밋하지 않는다). 예제의 값은 지어낸 것이다. 이미 있는 파일은 덮어쓰지 않고 모두 쓰거나 하나도 쓰지 않는다. 쓴 뒤 `ingest inspect`, `ingest run -data-dir`, `local`의 명령을 이 폴더의 경로로 출력한다.

**6. 로컬 모드: `praxium local -data-dir <폴더>` (REQ-294).** 폴더의 KG를 내장 파일 스토어로 열어 127.0.0.1(`-addr`)에서 API와 웹 UI를 낸다. 데모와 같은 조립을 쓴다. `demo.go`의 서버 시작을 `session`(주소, 데이터 폴더, 적재, 안내)으로 꺼내 두 명령이 공유한다. 실행마다 무작위 토큰이고 접속 주소의 프래그먼트가 `#/connect?token=…&mode=local`이다(데모는 `&demo=1`). 웹 UI가 이 표시로 "자기 데이터를 보는 중"임을 알린다. 합성 데이터를 넣지 않는다. 폴더나 KG가 없으면 먼저 `ingest run -data-dir`을 하라고 안내하고 끝난다(없는 폴더는 만들지 않는다). 포트는 적재 전에 잡고, 루프백이 아닌 주소는 경고한다. 시작 출력은 한 사람이 자기 컴퓨터에서 쓰는 용도이고 인증이 약하며 운영용이 아님을 밝힌다. 인증이 약하다는 점은 데모와 같다(실행마다 새 토큰, 평문 HTTP). 데모와 `local`의 시작이 5초 넘게 걸리면 5초마다 한 줄로 진행을 알린다. 접속 링크와 안내는 표준 출력, JSON 로그와 경고는 표준 오류로 나뉘어 있다(이미 그렇게 나뉘어 있었고 테스트가 그것을 단언한다).

**7. 그 밖 (REQ-295).** `/v1` 응답에 `X-Content-Type-Options: nosniff`와 `Referrer-Policy: no-referrer`를 붙인다. `Content-Security-Policy`는 브라우저가 문서로 그리는 응답을 다스리는 것이고 API의 응답은 JSON뿐이라 두지 않는다. `SHA256SUMS`는 파일 이름순이다. 릴리스 확인 스크립트가 `local`과 `ingest init → ingest run -data-dir → local` 흐름을 확인한다.

### 못 고친 것: 잠긴 테스트와 부딪친 곳

| 요구 | 부딪친 테스트 | 처리 |
|---|---|---|
| `api serve -h`의 종료 코드 0 | `TestApiUsageStaysForUnknownSubcommands`가 `api serve -h`의 종료 코드 1을 단언한다 | 도움말은 출력하고 종료 코드는 1로 둔다. 바꾸려면 요구사항 변경과 `amends_tests`가 필요하다 |
| `api serve`의 포트 선점 | `TestADevelopmentServerRefusesATruthFileItCannotLoad`가 적재가 실패하면 listen을 한 적이 없을 것을 단언한다 | `api serve`는 그대로 두었다. 데모와 `local`은 포트를 먼저 잡는다 |
| 운영 모드 오류에서 부채 번호 빼기 | `TestProductionNamesWhatItLacks`가 `-mode production`의 오류에 `TD-0010`, `TD-0011`을 요구한다 | `-mode`를 주지 않았을 때는 번호 없는 짧은 안내만, `-mode production`을 직접 줬을 때는 안내 뒤에 세부 문장을 괄호로 붙인다 |
| 모르는 플래그의 사용법 전체 | 데모, 수집, 월드젠의 테스트가 `usage: praxium <명령>`을 요구한다 | 사용법 한 줄과 도움말 위치 한 줄만 낸다(전체 목록은 아님) |

### 바뀐 설정과 보호 경로

| 경로 | 바뀐 것 | 이유 |
|---|---|---|
| `AGENTS.md` | 실행 예시를 릴리스 사용자가 따라 할 수 있게(`ingest init`, `-data-dir`, `local`) | 소스 체크아웃의 샘플 경로를 쓰지 않는다 |
| `docs/product-specs/requirements.md` | REQ-289~295 추가 | 요구사항 표의 변화 |
| `harness/config/test-lock.json` | 새 테스트의 지문(`pnpm lock-tests`가 쓴다) | 새 테스트의 잠금. 기존 항목은 바뀌지 않았다 |
| ADR-0033, ADR-0040 | `amended_by`와 수정한 부분 | 이 결정이 일부를 고친다 |

`harness/config`의 규칙, 한도, 허용 목록, 잠긴 테스트는 느슨하게 하지 않았다. 새 유닛과 새 의존성이 없다.

## 이유

- 첫 사용의 비용은 "무엇을 칠 수 있는지 아는 것"에서 먼저 든다. 도움말이 플래그 집합에서 만들어져야 코드와 갈라지지 않고, 사용법 오류가 짧아야 읽힌다.
- 오류의 원인은 서버가 이미 알고 있다. 계약에 새 필드를 만들지 않고 열려 있는 `params`에 채우면 호환성을 깨지 않고 명령줄과 웹 UI가 같은 값을 쓴다.
- 수집 결과가 남아야 "자기 파일을 수집해 웹 UI로 본다"가 성립한다. 수집 상태가 같이 남아야 사라진 행의 퇴역 규칙이 실행을 넘어 이어진다. 상태가 남지 않으면 다음 실행은 모든 행을 처음 보는 것으로 알고 객체를 다시 만든다.
- 데모와 로컬 모드의 차이는 적재와 안내뿐이므로 서버를 세우는 코드를 나누지 않는다.

## 대안과 기각 사유

- **수집 상태를 KG 안에 두기.** 아직 스토리지 계약에 수집 상태의 자리가 없다. 같은 폴더의 파일은 KG와 같은 잠금 아래에 있고 PostgreSQL 어댑터(TD-0011)가 상태의 영속을 이어받을 때 같은 포트(`StateStore`) 뒤에서 바뀐다.
- **KG만 남기고 상태는 다음 실행에서 다시 만들기.** 퇴역 판단과 식별 색인이 사라져 객체를 두 번 만들거나 한 번도 퇴역시키지 못한다.
- **예제를 `cmd/praxium`에 복사해 두기.** 같은 샘플이 두 곳에 있게 되고 합격 테스트가 보증하는 파일과 사용자가 받는 파일이 갈라진다.
- **새 유닛 `ingestexample`.** 샘플이 이미 한 패키지에 있고 새 유닛은 `harness/config`의 변경이 필요하다.
- **계약에 오류 필드(`target`, `reason`)를 새로 만들기.** `params`가 이미 "화면이 메시지를 쓸 때 쓰는 값"이다. 새 필드는 생성 코드와 적합성 검사를 함께 바꾸고 얻는 것이 없다.
- **`api serve`의 종료 코드와 포트 선점을 위해 잠긴 테스트를 고치기.** 요구사항이 바뀐 것이 아니라 새 편의가 오래된 테스트와 부딪친 것이다. 제품 책임자가 판단할 일이라 코드를 테스트에 맞췄다.

## 결과

- 한계. 상태 파일은 소스마다 전체 상태를 JSON으로 다시 쓴다. 파일 소스(20 MB 한도)에서는 문제가 없고 큰 스트림 소스는 이 방식이 아니라 스토리지가 상태를 가진다(TD-0011).
- 한계. 적용을 끝낸 뒤 상태를 저장하기 전에 프로세스가 죽으면 KG가 상태보다 한 실행 앞선다. 다음 실행은 같은 배치를 다시 계산한다(수집 파이프라인의 기존 순서이고 ADR-0033이 이미 밝혔다). 폴더를 지우고 다시 하면 복구된다.
- 한계. KG 이름은 `main`으로 고정되고 폴더 하나에 KG 하나이다. 한 폴더에 여러 소스를 넣을 수 있다.
- 로컬 모드는 데모와 같이 약한 보호이다(실행마다 새 토큰, 평문 HTTP). 운영 모드가 아니다.
