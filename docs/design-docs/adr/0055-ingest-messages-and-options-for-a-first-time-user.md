---
title: ADR-0055 수집의 소음과 메시지를 고친다 — 키 없는 행을 건너뛰는 옵션, 걸러진 행의 칸 문제, 무엇을 고칠지 알려 주는 메시지
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOCOV001
  - TLOCK001
  - TRACE006
  - backend/internal/ingest/messages_test.go
  - backend/cmd/praxium/ingestmessages_test.go
  - backend/internal/cli/linktypeview_test.go
updates_docs:
  - docs/guides/ingest-definitions.md
  - docs/product-specs/requirements.md
touches_protected:
  - docs/product-specs/requirements.md
  - harness/config/test-lock.json
decision_hash: 00dd0de661b42731
---
# ADR-0055 수집의 소음과 메시지를 고친다

## 맥락

독립 검증 3차에서, 처음 쓰는 사람이 자기 엑셀을 넣을 때 **소음**이 많고 **무엇을 고쳐야 하는지 알려 주지 않는** 메시지가 있었다. ADR-0052가 동작(무엇이 객체가 되는가)을 고쳤으므로 이 결정은 동작을 바꾸지 않는다. 바뀌는 것은 문제로 세는 범위, 선택 옵션 하나, 메시지의 내용이다. 기존 정의 파일은 그대로 동작하고 새 필드는 모두 선택이다.

1. 표 아래의 소계·합계 줄은 자연 키 칸이 비어 `unreadable_line`이 된다. 줄마다 문제가 하나씩 나오고 건너뛸 방법이 없었다.
2. 정제의 `filter`가 걸러낼 행의 칸이 `#N/A`이거나 수식이 계산된 적 없으면 그 행은 아무도 쓰지 않는데도 `cell_error`, `formula_without_value`가 나왔다. 걸러질 행인지는 정제 뒤에야 알 수 있으므로 읽는 쪽에서는 구분하지 못했다.
3. 쓰지 않는 컬럼이 많으면 `undeclared_column`이 컬럼마다 한 줄씩 나와 화면의 20줄을 채웠다. 줄 끝에 옵션 안내가 없었고, 잘린 뒤에는 전체 개수가 보이지 않았다. 데이터에 관한 진짜 문제가 그 뒤로 밀렸다.
4. 타입 이름을 틀리면 비슷한 이름이 없을 때 아무 안내가 없었다.
5. `link_target_missing`은 어느 값을 못 찾았는지 말하지 않았다.
6. BOM이 없는 UTF-16(엑셀의 "유니코드 텍스트")은 UTF-8이 아니어서 EUC-KR이나 CP949라고 잘못 추정했다.
7. 링크 타입의 `types get` 표는 `bindings`를 `(from=… to=…)`로 이어 붙여 읽을 수 없었다.
8. 정의 JSON의 값 종류가 틀리면 Go의 내부 오류(`cannot unmarshal string into Go struct field …`)가 그대로 나왔다.
9. `cardinality`와 `source_conflict`는 데이터셋도 행도 말하지 않고 내부 ID를 보였다. 칸 문제는 행의 키를 말하지 않았다. `identity_conflict`는 식별자 방식을 내부 ID(`is.…`)로 말했다.
10. `-emit-source` 초안을 그대로 실행하면 자연 키 자리표시자 오류가 컬럼 전체를 나열했다. 초안을 만들 때 표준 오류로 알려 준 후보가 이 오류에는 없었다.

## 결정

**1. 키 없는 행의 건너뛰기 (REQ-382).** 데이터셋에 `ignore_rows_without_key`(기본 `false`)를 더한다. 켜면 자연 키 컬럼이 **모두** 빈 행을 문제 없이 건너뛰고 요약 줄에 `skipped_no_key=N`으로 센다. 건너뛴 행은 `received`에 들어가지 않는다(`skipped_blank_row`와 같다). 그래서 행 수의 식(`received = read + unreadable`, `read = used + filtered + problem + unused`)은 그대로 맞는다. 키 컬럼 일부만 빈 행은 어느 쪽이든 `unreadable_line`이다. 한 키 컬럼만 빈 것은 오타일 수 있고 빈 행 전체는 소계 줄일 가능성이 크기 때문이다. 이름은 `ignore_undeclared_columns`와 같은 결이다. 켜지 않았을 때 모든 키 컬럼이 빈 행의 첫 `unreadable_line` 줄이 옵션 이름을 알려 준다(데이터셋마다 한 번). 건너뛴 행의 칸 문제도 알리지 않는다. csv, json_lines, xlsx 모두에 적용한다.

**2. 걸러진 행의 칸 문제 (REQ-383).** 읽는 쪽은 칸 문제(`cell_error`, `formula_without_value`)를 행에 붙여 두고(`Row.Issues`), 실행이 행을 모두 분류한 뒤에 알린다. **걸러졌고 어떤 피드도 쓰지 않았고 문제로 빠지지도 않은 행**의 칸 문제는 버린다. 다른 피드가 쓰는 행, 문제로 빠진 행, 쓰는 피드가 없는 행의 칸 문제는 그대로 알린다. 읽을 수 없는 줄(키가 비어 읽지 못한 줄)의 칸 문제도 그대로 알린다. 이유가 그 칸일 수 있기 때문이다. 같은 키가 겹쳐 버려진 앞의 행의 칸 문제도 그대로다. 파서가 돌려주는 `Table.CellIssues`는 건드리지 않는다(잠긴 테스트가 읽는다). 칸 문제는 이제 문제 목록의 뒤쪽에 나올 수 있다.

**3. `undeclared_column` (REQ-384).** 데이터셋마다 첫 줄이 `ignore_undeclared_columns`를 알려 준다(줄마다 되풀이하지 않는다). 목록이 20줄에서 잘리면 안 보인 `undeclared_column`의 수와 전체 개수를 한 줄로 보인다. 화면 목록에서는 `undeclared_column`을 다른 문제 뒤에 둔다(보고서 자체의 순서는 그대로다). 컬럼 수십 개의 대장에서 데이터 문제가 밀려 나지 않게 하기 위해서다. 보고서가 앞의 200개만 담을 때도 한 줄로 말한다.

**4. 틀린 타입 이름 (REQ-385).** 비슷한 이름이 있으면 지금처럼 제안하고, 없으면 `praxium types list --all`(링크는 `--kind link`)을 알려 준다. 서버 쪽의 `query find --type` 오류(`no object type "X"`)는 고치지 않았다. 그 문구는 CLI와 서버의 잠긴 테스트(`backend/internal/cli/cause_test.go`, `backend/internal/httpapi/errordetail_test.go`)가 글자 그대로 단언하고 응답의 `params.message` 계약에 들어 있어서, 고치려면 계약과 잠긴 테스트를 바꿔야 한다. 이 결정의 범위 밖이라 보고만 한다.

**5. `link_target_missing` (REQ-386).** 메시지가 찾지 못한 값(따옴표로 감싼 것), 이쪽 컬럼, 저쪽 데이터셋과 컬럼을 말하고 오타인지 저쪽 파일에 행이 없는 것인지 보라고 한다.

**6. BOM 없는 UTF-16 (REQ-387).** UTF-8이 아닌 파일의 원인을 추정하기 전에 앞 2KB에서 NUL 바이트를 센다. 한쪽(홀수나 짝수 위치)에 NUL이 있고 다른 쪽에 거의 없으면(30% 이상과 5% 이하) 그 순서(little-endian, big-endian)와 함께 UTF-16으로 보인다고 말한다. 텍스트 파일에는 NUL이 없으므로 오탐이 낮다. 해결 방법(UTF-8로 다시 저장)은 그대로다. 글자를 바꿔 읽지는 않는다(ADR-0046).

**7. 링크 타입의 `types get` 표 (REQ-388).** `bindings`를 `PhysicalServer -> Rack  cardinality N:1`처럼 줄마다 보인다. 이름은 객체 타입과 트레이트 목록에서 읽어 오고(요청 둘), 읽을 수 없으면 ID의 끝을 보인다. 트레이트 쪽은 "any object with the trait T"이다. `--json`은 서버의 답 그대로다.

**8. 정의 JSON의 값 종류 (REQ-389).** `the field "datasets[0].ignore_undeclared_columns" must be true or false (no quotes), but the value given is text (line 5)`처럼 필드 경로(목록의 자리는 `[0]`), 받아야 하는 값의 종류, 받은 값의 종류와 줄 번호를 말한다. 소스와 매핑 정의 모두에 적용한다.

**9. 위치 정보 (REQ-390, REQ-391).** 칸 문제(`cell_error`, `formula_without_value`)에 행의 자연 키를 넣는 것은 잠긴 테스트의 출력을 바꾸므로 이번에는 하지 않았다(다음에 따로 결정한다). `cardinality`와 `source_conflict` 문제는 데이터셋과 행의 키, 줄(또는 셀 범위)을 담고, 내부 ID 대신 링크 타입과 객체의 이름으로 말한다. 진 값의 `source_conflict`는 이긴 값의 행도 말한다. `source_conflict`의 `key`는 이제 객체 ID가 아니라 진 값이 있던 행의 자연 키이고, 메시지 본문에 객체의 이름과 ID를 함께 적는다. `identity_conflict`의 메시지는 식별자 방식을 내부 ID가 아니라 이름(`bmc_mac`)으로 말한다.

**10. `-emit-source` 초안의 실행 (REQ-392).** 자연 키가 자리표시자인 소스 정의를 실행해 멈출 때, 그 데이터셋에 `-input`으로 준 파일을 데이터셋의 시트·머리글 행·구분자 설정대로 다시 살펴서 모든 행에 값이 있고 서로 다른 컬럼을 컬럼 전체 목록보다 먼저 말한다. 초안을 만들 때 쓴 같은 계산(`keyCandidates`)이다. 파일을 주지 않았거나 읽을 수 없으면 지금의 오류 그대로다.

**문서 (REQ-393).** 형식 안내서와 실행 파일에 담는 사본을 새 옵션과 메시지에 맞췄고, 7절의 예시 출력은 빌드한 실행 파일로 다시 돌려 확인했다. 안내서는 400줄 한도 안이라 문서를 나누지 않았다. `ingest run --help`가 `skipped_no_key`를 설명한다.

## 이유

- **소음은 신뢰를 깎는다.** 처음 쓰는 사람의 첫 실행에서 소계 줄과 쓰지 않는 컬럼이 문제 수십 줄을 만들면 진짜 문제가 묻히고, 도구가 시끄럽기만 하다고 느낀다. 해결책을 메시지가 말해야 한다.
- **걸러진 행은 아무도 쓰지 않는다.** 사용자가 규칙으로 뺀 행의 칸을 고치라고 하는 것은 틀린 안내이다. 다만 걸러질 행인지는 정제 뒤에야 알 수 있으므로 알리는 때를 분류 뒤로 미뤘다.
- **동작은 그대로 둔다.** 객체가 되는 행과 요약 줄의 식은 달라지지 않는다. 새 옵션은 기본이 꺼짐이라 기존 정의 파일의 결과가 같다.

## 대안과 기각 사유

- **키 컬럼 일부만 빈 행도 건너뛰기.** 오타로 키 하나가 빈 행을 조용히 지운다. 모두 빈 행만 건너뛴다.
- **건너뛴 행을 `unused`나 `problem`으로 세기.** 사용자가 의도해서 뺀 행이므로 `received`에 넣지 않는다. 빈 행의 `skipped_*`와 같은 자리이다.
- **걸러진 행의 칸 문제를 읽을 때 버리기.** 읽는 쪽은 정제를 모른다. 정제 규칙을 읽는 쪽으로 옮기면 계층이 섞인다.
- **서버의 타입 오류 문구 고치기.** 응답 계약과 잠긴 테스트를 바꿔야 해서 하지 않았다.
- **비슷한 이름이 없을 때 모든 타입 이름을 나열하기.** 목록이 길어 메시지가 읽기 어렵다. 명령 이름이 낫다.

## 결과

- 새 의존성이 없다. `harness/config`는 바꾸지 않았다. 바뀐 보호 경로는 요구사항 표(REQ-382~393 추가)와 잠긴 테스트(`test-lock.json`)이다.
- 잠긴 테스트는 바꾸지 않았다.
- 문제 줄의 순서가 조금 바뀐다. 칸 문제는 행을 분류한 뒤에 알려서 문제 목록의 뒤쪽에 나올 수 있고, 화면 목록은 `undeclared_column`을 마지막에 둔다. `source_conflict`의 `key`는 객체 ID에서 행의 자연 키로 바뀐다. `graph_hash`와 객체 ID는 달라지지 않는다.
- 알려진 한계. `types get`의 링크 타입 표는 타입 이름을 위해 요청을 둘 더 보낸다. 서버의 `query find --type` 오류 문구는 그대로다(보고).
