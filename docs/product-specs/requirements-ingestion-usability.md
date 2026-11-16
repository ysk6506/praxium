---
title: 프락시움 요구사항 — 수집의 사용성과 메시지
kind: spec
status: active
follows:
  - docs/design-docs/adr/0005-separate-product-own-model-verification-outside.md
---
# 요구사항 — 수집의 사용성과 메시지

처음 쓰는 사람을 위한 수집 명령, 옵션, 메시지, 정의 형식 안내서의 요구사항이다. 목표와 REQ 번호의 규칙은 [요구사항 입구](requirements.md)에 있다. 번호는 모든 요구사항 문서에서 하나뿐이다.

| ID | 요구사항 | 근거·테스트 |
|---|---|---|
| REQ-288 | `praxium ingest inspect -input <파일>`은 CSV, JSON Lines, 엑셀 파일의 프로파일을 표와 `-o json`으로 낸다: 컬럼마다 이름, 값 타입, 빈 값 비율, 고유 값 수, 빈출 값, 값의 형태, 처음 몇 행. 엑셀은 시트 목록(크기, 숨김 상태)과 추정한 머리글 행을 더한다. 파일을 쓰거나 매핑을 적용하지 않는다 | ADR-0040, `backend/internal/ingest/profile_test.go`, `backend/cmd/praxium/inspect_test.go` |
| REQ-291 | `praxium ingest run -data-dir <폴더>`는 내장 파일 스토어의 그 폴더에 KG(`main`)를 만들거나 이어 쓰고, 수집 상태(원본 레코드 구간, 안 보인 횟수, 식별 색인)를 같은 폴더의 버전이 있는 JSON 파일에 원자적으로 저장한다(임시 파일, fsync, 이름 바꾸기). 다음 실행은 다른 프로세스에서도 그 상태에서 이어 가므로 사라진 행의 퇴역 판단이 실행을 넘어 이어진다. KG와 상태가 맞지 않는 폴더(상태가 없는 KG, KG가 없는 상태, 다른 KG의 상태)는 아무것도 바꾸지 않고 거절한다. 다른 프로세스가 연 폴더는 잠금 오류와 해결 방법을 알린다 | ADR-0041, `backend/internal/ingest/statefile_test.go`, `backend/cmd/praxium/ingestkeep_test.go` |
| REQ-292 | `ingest run`은 소스 정의에 없는 데이터셋 이름의 `-input`을 종료 코드 2의 오류로 거절하고 정의에 있는 이름을 알린다. `-input`이 없는 데이터셋은 수집하지 않고 그 사실을 알리며 이전 데이터는 그대로 둔다. 문제를 따로 둔 실행은 기본이 종료 코드 0이고 마지막 줄에 문제 수를 요약하며, `-fail-on-problem`이면 고유 종료 코드 20이다. `-data-dir` 없이 실행하면 결과가 남지 않는다는 것과 방법을 마지막에 알린다. `--help`는 출력의 항목(`volatile`, `graph_hash`, `raw_added` 등)과 종료 코드 표를 설명한다 | ADR-0041, `backend/cmd/praxium/ingestkeep_test.go` |
| REQ-293 | `praxium ingest init -out <폴더>`는 실행 파일에 담긴 예제를 쓴다: CSV와 JSON Lines의 소스 정의, 매핑 정의, 샘플 파일, 엑셀의 소스 정의와 매핑 정의와 샘플 워크북(그 자리에서 만든다). 이미 있는 파일은 덮어쓰지 않고 모두 쓰거나 하나도 쓰지 않는다. 다음에 칠 명령(`ingest inspect`, `ingest run -data-dir`, `local`)을 출력한다 | ADR-0041, `backend/cmd/praxium/ingestinit_test.go` |
| REQ-320 | 수집은 파일에 없는 시트와 선언한 컬럼이 빠진 파일을 조용히 지나치지 않는다. 그 데이터셋은 `shape_changed` 문제이고 데이터셋 줄에 `not_read`로 이유가 나온다. 시트가 없으면 파일에 실제로 있는 시트 이름을, 컬럼이 없으면 실제로 읽은 머리글(시트는 머리글 행 번호)을 메시지에 싣는다. 문제 수에 잡히고 `-fail-on-problem`이면 종료 코드 20이다 | ADR-0046, `backend/internal/ingest/usability_test.go`, `backend/cmd/praxium/ingestusability_test.go` |
| REQ-321 | 식별 키, 식별자 스킴, 속성, 컬럼, 객체 타입과 링크 타입의 이름이 틀린 오류는 가능한 값을 싣는다. 키 오류는 그 타입이 허용하는 식별 키 전부(속성 조합, 링크가 낀 조합, 스킴)와 매핑에 쓰는 방법을, 스킴 오류는 그 타입의 키인 스킴과 모델의 모든 스킴을, 이름 오류는 비슷한 이름을 말한다. 질의의 `identifier.scheme` 오류(CLI의 `--identifier`)도 모델의 스킴 목록을 싣는다 | ADR-0046, `backend/internal/ingest/usability_test.go`, `backend/internal/model/hints_test.go`, `backend/internal/query/schemehint_test.go` |
| REQ-322 | `ingest init`의 예제(엑셀과 CSV)는 서버의 BMC MAC을 `identifiers`로 채워 수집한 객체의 상세와 테이블에 식별자가 보인다. `--identifier bmc_mac:<값>`은 원장에 적힌 모양(콜론이나 하이픈이 든 MAC)을 스킴의 정규화로 맞춰 그 객체를 찾는다 | ADR-0046, `backend/cmd/praxium/ingestusability_test.go`, `backend/internal/query/schemehint_test.go` |
| REQ-323 | UTF-8이 아닌 CSV와 JSON Lines는 깨진 글자로 읽지 않고 `ingest inspect`와 `ingest run` 모두 오류로 멈춘다. 오류는 무엇이 문제인지(로컬 코드 페이지, UTF-16 바이트 순서 표시)와 해결 방법("CSV UTF-8"로 다시 저장하거나 `.xlsx`를 그대로 쓴다)을 말한다. UTF-8 BOM은 읽는다. 로컬 코드 페이지(EUC-KR, CP949)를 직접 읽지는 않는다 | ADR-0046, `backend/internal/ingest/usability_test.go`, `backend/cmd/praxium/ingestusability_test.go` |
| REQ-324 | CSV 데이터셋은 구분자 옵션 `delimiter`(쉼표가 기본, 탭, 세미콜론, 세로선을 문자나 이름으로)를 가진다. `ingest inspect -delimiter`가 같은 값을 받고, 쉼표로 읽어 한 컬럼이 되고 그 값에 탭, 세미콜론, 세로선이 반복되면 구분자가 다른 것 같다고 알린다. 수집에서 컬럼이 빠진 오류에도 같은 안내가 붙는다 | ADR-0046, `backend/internal/ingest/usability_test.go`, `backend/cmd/praxium/ingestusability_test.go` |
| REQ-325 | 데이터셋의 `ignore_undeclared_columns`를 켜면 선언하지 않은 컬럼을 `undeclared_column` 문제로 보고하지 않는다(기본은 보고). 선언한 컬럼이 파일에서 사라진 것은 옵션과 상관없이 계속 보고한다 | ADR-0046, `backend/internal/ingest/usability_test.go` |
| REQ-326 | `ingest inspect -emit-source`는 파일(시트마다)의 소스 정의 초안을 표준 출력에 JSON으로 낸다: 시트, 머리글 행, 컬럼, 값 종류의 추측(`note`). 안내는 표준 오류에 낸다. 자연 키는 추측하지 않고 플레이스홀더 문구를 두며, 그대로 `ingest run`에 넣으면 소스의 컬럼 목록과 함께 무엇을 채울지 알리는 오류로 멈춘다. 자연 키를 채운 초안은 그대로 수집된다 | ADR-0046, `backend/internal/ingest/usability_test.go`, `backend/cmd/praxium/ingestusability_test.go` |
| REQ-327 | 정의 JSON의 `unknown field` 오류는 그 위치와 거기서 허용되는 필드를 싣는다. `missing_natural_key`는 비어 있는 컬럼 이름을 말한다. 서버에 묻지 않는 명령(`ingest` 등) 앞에 `--endpoint`, `--token` 같은 전역 플래그를 쓰면 그 명령이 서버에 접속하지 않아 받지 않는다고 원인을 말한다 | ADR-0046, `backend/internal/ingest/usability_test.go`, `backend/cmd/praxium/ingestusability_test.go` |
| REQ-329 | `ingest`와 `ingest run --help`는 정의 형식의 안내서(docs/guides/ingest-definitions.md)를 가리키고, `ingest run --help`는 요약 줄의 항목(`not_read`, `skipped_blank_row`, `skipped_hidden_row`, `skipped_after_end`와 문제의 종류)을 한 줄씩 설명한다 | ADR-0046, `backend/cmd/praxium/ingestusability_test.go` |
| REQ-330 | `ingest init`은 예제 폴더에 형식 안내서(`ingest-definitions.md`)도 쓴다. 쓰는 사본은 docs/guides/ingest-definitions.md와 같다 | ADR-0046, `backend/cmd/praxium/ingestguide_test.go` |
| REQ-331 | 새 옵션(`delimiter`, `ignore_undeclared_columns`, 컬럼의 `note`)은 모두 선택이다. 옛 정의 파일은 그대로 읽히고 같은 입력이면 같은 `graph_hash`가 나온다 | ADR-0046, `backend/internal/ingest/usability_test.go` |
| REQ-364 | 수집 정의 형식 안내서(docs/guides/ingest-definitions.md)와 실행 파일에 담는 사본이 새 동작(행이 빠지는 경우와 속성만 빠지는 경우, `key_name`의 뜻, 걸러진 행의 객체가 닫히는 규칙, 행 수 한도, 요약 줄의 새 항목)과 실제 출력에 맞고 서로 같다. `ingest run --help`가 새 요약 항목을 설명한다 | ADR-0052, `backend/cmd/praxium/ingestguide_test.go`, `backend/cmd/praxium/ingestkeep_test.go` |
| REQ-382 | 데이터셋의 `ignore_rows_without_key`(기본 꺼짐)를 켜면 자연 키 컬럼이 모두 빈 행(소계·합계 줄)을 문제 없이 건너뛰고 요약 줄의 `skipped_no_key`에 센다(`received`에 넣지 않는다). 켜지 않으면 지금처럼 `unreadable_line`이고 첫 문제 줄이 옵션 이름을 알려 준다. 키 컬럼 일부만 빈 행은 어느 쪽이든 `unreadable_line`이다 | ADR-0055, `backend/internal/ingest/messages_test.go`, `backend/cmd/praxium/ingestmessages_test.go` |
| REQ-383 | 정제(`filter`)로 걸러진 행의 칸 문제(`cell_error`, `formula_without_value`)는 문제로 세지 않는다. 다른 피드가 쓰는 행이나 걸러지지 않은 행의 칸 문제는 그대로 나온다. 행 수의 식(`received = read + unreadable`, `read = used + filtered + problem + unused`)은 계속 맞는다 | ADR-0055, `backend/internal/ingest/messages_test.go` |
| REQ-384 | `undeclared_column`의 첫 문제 줄이 `ignore_undeclared_columns`를 알려 주고, 문제 목록이 잘리면 안 보인 `undeclared_column`의 수와 전체 개수를 한 줄로 보인다. 데이터에 관한 문제가 `undeclared_column`보다 먼저 보인다 | `backend/cmd/praxium/ingestmessages_test.go` |
| REQ-385 | 매핑의 `object_type`이나 링크 타입 이름이 틀렸을 때 비슷한 이름이 있으면 제안하고, 없으면 타입 목록을 보는 명령(`praxium types list --all`, 링크는 `--kind link`)을 알려 준다 | ADR-0055, `backend/internal/ingest/messages_test.go` |
| REQ-386 | `link_target_missing`의 메시지는 찾지 못한 값과 그 값이 있는 컬럼(이쪽), 찾은 컬럼(저쪽 데이터셋)을 말한다 | ADR-0055, `backend/internal/ingest/messages_test.go` |
| REQ-387 | UTF-8이 아닌 파일의 원인을 추정할 때 NUL 바이트가 하나 걸러 하나씩 규칙적으로 있으면(BOM이 없는 UTF-16) UTF-16이라고 말한다. 해결 방법 안내는 그대로다 | ADR-0055, `backend/internal/ingest/messages_test.go` |
| REQ-388 | 링크 타입의 `types get` 표는 `bindings`를 양쪽 타입 이름과 허용하는 수가 있는 줄로 보인다(`PhysicalServer -> Rack  cardinality N:1`). `--json`은 서버의 답 그대로다 | `backend/internal/cli/linktypeview_test.go` |
| REQ-389 | 정의 JSON의 값 종류가 틀리면(`"ignore_undeclared_columns": "yes"`) Go의 내부 오류 대신 필드 경로, 받아야 하는 값의 종류, 받은 값의 종류와 줄 번호를 사람의 말로 알려 준다 | ADR-0055, `backend/internal/ingest/messages_test.go` |
| REQ-390 | `cardinality`와 `source_conflict` 문제는 데이터셋과 행(엑셀이면 셀 범위)을 말한다. `cardinality`는 내부 ID 대신 링크 타입과 객체의 이름을 쓴다 | ADR-0055, `backend/internal/ingest/messages_test.go` |
| REQ-391 | `identity_conflict`의 메시지는 식별자 방식을 내부 ID가 아니라 매핑에 쓰는 이름으로 말한다. `source_conflict`의 메시지는 객체의 이름과 ID를 함께 말한다 | ADR-0055, `backend/internal/ingest/messages_test.go` |
| REQ-392 | `-emit-source` 초안을 그대로 `ingest run`하면 나오는 자연 키 오류가 그 데이터셋에 `-input`으로 준 파일에서 모든 행에 값이 있고 서로 다른 컬럼(초안을 만들 때 알려 준 후보)을 컬럼 전체 목록보다 먼저 말한다. 그 데이터셋에 준 파일이 없으면 지금의 오류 그대로다 | `backend/cmd/praxium/ingestmessages_test.go` |
| REQ-393 | 수집 정의 형식 안내서(docs/guides/ingest-definitions.md)와 실행 파일에 담는 사본이 새 옵션과 새 메시지에 맞고 서로 같다. `ingest run --help`가 `skipped_no_key`를 설명한다 | ADR-0055, `backend/cmd/praxium/ingestmessages_test.go`, `backend/cmd/praxium/ingestguide_test.go` |
| REQ-419 | **예정(ADR-0058)** 연습용 소스 명령(`praxium sample-source`)이 합성 세계를 읽기 전용 HTTP API(토큰 발급, 쪽 번호와 전체 개수, 본문의 다음 주소를 가진 경로)로 내고 PostgreSQL에 넣는 SQL(읽기 전용 역할을 만드는 문장 포함)을 낸다. 그 소스에 맞는 소스 정의와 매핑 정의의 예시를 함께 내고, 안내서와 끝에서 끝까지의 테스트가 이것을 쓴다. 테스트의 주소는 예약된 이름(`example.test` 등)만 쓴다 |
