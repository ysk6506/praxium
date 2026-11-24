---
title: ADR-0103 엑셀 시트를 행마다 읽기 — 포트와 스캐너와 행 파서와 한도
kind: decision
status: active
enforced_by:
  - TLOCK001
  - GOARCH010
  - backend/internal/ingest/sheetscan_equal_test.go
  - backend/internal/ingest/xs_changed_test.go
  - backend/internal/ingest/sheetscan_test.go
  - backend/internal/ingest/sheetscan_memory_test.go
touches_protected:
  - docs/design-docs/adr/0040-excel-workbook-source-read-with-the-standard-library.md
  - docs/product-specs/requirements-ingestion.md
  - harness/config/test-lock.json
updates_docs:
  - docs/product-specs/requirements-ingestion.md
  - docs/guides/ingest-definitions.md
  - docs/design-docs/ingestion.md
amends:
  - adr: "0040"
    what: 워크북을 바이트로 통째로 읽는 대신 시트를 행마다 읽는 포트와 새 한도(결정 2와 5)
decision_hash: 56a4bf9b6be0b733
---
# ADR-0103 엑셀 시트를 행마다 읽기 — 포트와 스캐너와 행 파서와 한도

이 결정은 세 변경(PR 1~3)이 함께 쓰는 기록이다. 첫 변경(스캐너)이 이 문서를 만들고 결정 전체를 적는다. 뒤의 변경이 구현하는 것은 "뒤의 PR"로 표시한다. 새 흐름에서 엑셀 데이터셋을 페이지로 읽는 일(페이지, 완전성, 파일 열기, 점검)은 다음 기록이 정한다.

## 맥락

엑셀 소스는 파일 바이트를 모두 메모리에 올려 ZIP을 풀고, 시트의 셀 전부(`[]SheetRow`)를 올린 뒤 `ParseSheet`가 표로 바꾼다(ADR-0040). 메모리가 행 수에 비례해서 100만 행 × 20열이면 약 3.3 GB로 어림되고, 새 수집 흐름(ADR-0086)이 약속한 "읽는 쪽은 페이지 하나만 쥔다"와 맞지 않는다. 시트 하나를 행마다 읽어 페이지로 만들려면 시트를 표로 바꾸는 규칙이 시트 전체를 보지 않고 정해져야 한다. 독립 검토 세 차례를 거쳐 아래의 결정이 확정됐다.

## 결정

1. **읽기 포트**(뒤의 PR 3). `core.ingest`에 `WorkbookStreamer`(압축 파일을 `io.ReaderAt`과 크기로 열어 메모리에 두는 몫(시트 목록, 공유 문자열, 스타일)을 한도 안에서 읽는다), `OpenedWorkbook`(시트 목록과 시트 열기), `SheetStream`(시트 하나. 첫 읽기 `Layout`이 병합과 숨긴 열을 주고, 둘째 읽기 `Rows`가 행을 파일의 순서대로 준다)를 둔다. 어댑터(`adapter.xlsx`의 `Streamer`)가 구현하고 `core.ingest`는 파일도 ZIP도 모른다. 옛 포트(`WorkbookReader`, `Workbook`, `Sheet`)는 그대로다. 이 PR이 더하는 것은 포트가 쓰는 두 타입 `SheetHead`와 `SheetLayout`뿐이다.
2. **병합 때문에 시트 파트를 두 번 읽는다**(뒤의 PR 3). 병합 범위는 시트 XML의 `<sheetData>` 뒤에 있는데 머리글 이름과 `fill`이 쓴다. 첫 읽기는 `<sheetData>` 안쪽의 바이트를 버리고 남은 작은 XML에서 `<col>`과 `<mergeCell>`만 읽고, 둘째 읽기가 행을 낸다. 원형의 측정에서 첫 읽기는 행 읽기 시간의 9~10%였다. 토큰으로 건너뛰는 읽기는 80%라서 버렸다.
3. **시트의 행을 표의 행으로 바꾸는 의미의 구현은 하나다**(이 PR). `core.ingest`의 `SheetScanner`가 머리글, 읽는 컬럼, 빈 행, 데이터의 끝, 숨김, 병합의 값, 오류 셀과 수식, 시트 범위, 주소를 정한다. 옛 `ParseSheet`는 시트의 행을 스캐너에 차례로 넣고 `Table`로 모으는 얇은 함수가 되었다(`Table`의 모양과 `ParseSheet`의 이름과 서명은 그대로). 셀의 값을 글로 바꾸는 일(`convert`, `values.go`)은 `adapter.xlsx`의 행 파서 하나에 있고, 옛 `ReadWorkbook`이 그 위에 서도록 하는 일은 뒤의 PR 2이다. 옛 경로를 지울 때도 `ReadWorkbook`, `ParseSheet`, `Workbook`, `WorkbookReader`, `Inspect`는 지우지 않는다(`ingest inspect`와 소스 정의 초안이 쓴다).
4. **스캐너는 시트를 쥐지 않는다**(이 PR). 스캐너는 행을 파일의 순서대로 하나씩 받는다(`Row`). 머리글 행의 칸의 사본, 필요한 병합 범위의 첫 칸, 몇 개의 셈만 쥔다. 머리글은 첫 데이터 행이 오면(데이터 행이 없으면 시트의 끝에서) 정하고, 머리글에 읽을 컬럼이 없으면 읽기를 멈추게 한다(`ScanStep.Done`). 데이터 범위의 끝 행을 지나도 멈춘다. 필요한 병합 범위는 머리글 행에 닿는 것과, `fill`일 때 데이터에 닿고 읽는 컬럼을 덮는 것이다. 병합의 첫 칸은 그 범위의 첫 행이 오면 쥐고 마지막 행이 지나가면 돌려준다.
5. **머리글 이름은 흘려 읽을 수 있는 규칙으로 정한다**(이 PR). 컬럼의 머리글 이름은 머리글 행의 칸과, 머리글 행에 닿는 병합 범위가 **그 범위의 끝 열까지** 주는 글을 머리글 행마다 정규화해 공백으로 이은 것이다. 옛 코드는 시트의 사용 범위(모든 행의 마지막 열)까지 주었는데, 아래쪽 행을 알아야 정해지므로 흘려 읽을 수 없다. 이 규칙의 위험을 함께 막는다. 병합으로 이름을 받는 열은 칸이 가질 수 있는 마지막 열(`max_columns_per_sheet`의 기본값 1,024)에서 자르고 소스 정의로 올릴 수 없다. 한 병합의 첫 칸에서 온 이름은 한 번 정규화한 글 하나를 열마다 나눠 쓴다. 머리글 이름이 새로 만드는 글(여러 행을 잇는 이름)은 만들기 전에 길이를 셈에 넣는다. `reportExtra`는 보고가 싣는 문제의 수(`maxProblemSamples`)를 넘는 이름의 이유 글을 만들지 않는다(세기는 그대로다). 병합의 끝 열이 주는 이름이 늘 열마다 새 글이 되던 옛 방식에서는 16 KiB 글을 8,192열에 병합하면 최고 힙이 259 MiB이고, 병합 주소는 1,048,576열까지 받으므로 `A1:ZZZZ1`이면 약 14 GiB가 될 수 있었다.
6. **글을 만들지 않은 칸도 빈칸인지는 안다**(이 PR). 읽는 쪽이 읽지 않는 열의 글을 만들지 않으면(`Cell.Skipped`) 칸이 값을 가졌다는 것만 안다. 그런데 빈 행의 판단은 "값이 있다"와 다르다. 공백뿐인 글과 저장된 값이 없는 수식은 칸으로 남지만 빈칸이다. 그래서 `Cell`에 `Blank`를 더해, 글을 만들지 않은 칸에도 옛 빈칸 규칙의 판단을 싣는다. 스캐너는 글이 있는 칸은 글로, `Skipped` 칸은 `Blank`로 판단한다. 읽지 않는 열에만 공백이나 계산 안 된 수식이 있는 행이 `blank_row`로 세어지고, `stop_after_blank_rows` 뒤의 `after_end`의 수가 옛 경로와 같다. 읽는 쪽이 `Blank`를 싣는 일은 뒤의 PR 2와 3이다(공유 문자열은 읽을 때 문자열마다 "공백뿐" 비트를 만든다. 인라인 글은 `_xHHHH_`를 푼 뒤 유니코드 공백으로 본다).
7. **스캐너가 쥐는 글에는 고정한 한도가 있다**(이 PR). 머리글 행의 칸, 필요한 병합 범위의 첫 칸, 머리글 이름의 글의 합이 8 MiB(`max_kept_bytes`)를 넘으면 한도 이름을 말하는 `invalid_value` 오류이다. 소스 정의로 바꾸지 못한다. 스캐너의 오류는 `ParseSheet`에서 `Table.Reason`이 된다(결정 8).
8. **행의 순서를 본다**(스캐너는 이 PR, 행 파서는 뒤의 PR 2). 스트림은 정렬할 수 없다. 스캐너는 행 번호가 앞 행 이하이거나 한 행에서 칸의 열이 앞 칸 이하이면 `invalid_value`(`the workbook is damaged: the rows of sheet "S" are not in order (row 7 after row 9)`)로 멈춘다. `ParseSheet`는 오류를 돌려주지 않으므로 이 경우 `Table.Reason`으로 쓸 수 없는 표의 이유를 말한다(그 데이터셋은 `shape_changed`). 같은 번호의 행이 둘이면 옛 `ParseSheet`는 두 행을 모두 읽되 둘 다 뒤 행의 칸으로 읽고 빈 행을 −1로 세었다. 이것도 거절한다. 뒤의 PR 2가 행 파서에서 같은 것을 거절하면 옛 `ReadWorkbook`의 `Sorted()` 호출은 필요 없어진다(메서드는 잠긴 테스트가 부르므로 남긴다).
9. **메모리 목표**(이 PR은 머리글 입력, 뒤의 PR 3은 전체). 설계의 한도 안의 어떤 입력도 워크북 읽기 하나의 최고 힙(가비지 컬렉터의 여유 포함)이 256 MiB를 넘지 않는다. 살아 있는 힙의 합이 128 MiB 아래이면 제품의 기본 설정(`GOGC=100`)에서 성립한다. 테스트는 `SetGCPercent(25)`(최고 힙이 살아 있는 힙의 약 1.25배)에서 시작 때보다 늘어난 최고 힙을 2 ms 표본으로 3번 재어 가장 큰 값을 **160 MiB**와 견준다(`httpsource`의 같은 이름의 측정과 같은 방법. 임계값은 128 MiB × 1.25). 이 PR의 `sheetscan_memory_test.go`가 머리글 병합 공격 입력을 지금의 명령줄 경로(`ParseSheet`)에서 잰다. 프로세스의 상한은 수집 동시 4개와 점검 1개를 곱한 5 × 256 MiB = 1.25 GiB이고 `GOMEMLIMIT`은 이 결정 밖이다.
10. **메모리에 닿는 한도는 소스 정의가 올릴 수 없다**(뒤의 PR 3). `max_cell_chars`, `max_shared_strings`, `max_shared_string_bytes`, `max_merge_ranges`, `max_styles`는 기본값보다 크게 잡으면 정의 검사가 거절한다(내리는 것은 된다). 그래야 결정 9의 목표가 "설계의 한도 안"이라는 조건으로 성립한다. 읽개의 인자는 깎지 않고, 옛 `ReadWorkbook`의 오류 글(`raise limits.NAME`)은 그대로 둔다. 이미 올려 둔 정의는 옛 경로에서도 거절된다.
11. **공유 문자열**(뒤의 PR 3). 공유 문자열은 바이트 덩어리 하나와 `uint32` 오프셋과 "공백뿐" 비트로 쥔다. 무게가 글의 길이 + 4 바이트이고 가비지 컬렉터가 훑을 포인터가 없다. 덩어리는 공유 문자열 파트의 선언한 푼 크기와 한도 가운데 작은 쪽으로 **한 번에** 잡고 키우지 않는다(키우면 옛 배열과 새 배열이 함께 있어 최고 힙이 무게의 2.3배가 된다. 독립 테스트: 같은 입력에서 키우기 129 MiB, 한 번에 잡기 56 MiB). 모자라면 오류이다. `max_shared_string_bytes`는 덩어리의 무게로 **48 MiB**이다(문자열 100만 개는 평균 약 46 바이트까지, 200만 개는 약 21바이트까지 읽힌다).
12. **새 한도와 고정 값**(뒤의 PR 3). `Limits`에 `max_file_bytes`, `max_sheet_bytes`, `max_compression_ratio`, `max_shared_string_bytes`, `max_merge_ranges`, `max_styles`를 더하고 `WithStreamDefaults()`가 새 흐름의 기본값(`max_rows_per_sheet` 1,048,576)을 준다. `WithDefaults()`는 옛 값 그대로이다. 압축 파일 항목 수와 이름 길이, 시트와 관계와 숫자 서식의 수와 글의 길이, `<col>`의 수, XML 토큰과 깊이, 행의 글 합(`max_row_bytes` 4 MiB), 첫 읽기의 남은 바이트(`max_layout_bytes` 64 MiB)는 필드가 아닌 고정 값이다. 오류와 불완전의 기준은 한 가지이다. 워크북 전체에 닿는 한도는 오류이고, 한 시트나 한 데이터셋의 양(행, 열, 파일 크기)은 불완전이다. 셀 하나와 행 하나의 크기는 읽을 수 없는 입력이라 오류이다. 새 흐름의 페이지와 완전성은 다음 기록이 정한다.
13. **같음을 기대값 파일로 지킨다**(이 PR). 같음은 이 변경 직전의 코드(`d1df3c0`)로 만든 기대값 파일과 견주어 보인다. 손으로 쓴 사례는 표 전체(JSON)를, 시드가 정해진 무작위 시트 3,000개는 결과의 SHA-256만 `testdata`에 둔다. 만든 일회용 도구는 저장소에 두지 않는다(두면 새 코드로 다시 만들 수 있어 견줌이 무의미해진다). 글을 만들지 않은 칸(결정 6)은 같은 시트에서 읽지 않는 열의 칸을 `Skipped`로 바꾼 변형이 같은 표를 내는 것으로 본다. 바뀌는 의미(아래 표)는 손으로 쓴 기대값(`xs_changed_scan.json`)에 따로 둔다. 새 테스트의 도우미와 타입은 `xs` 접두사를 쓰고, **테스트 파일에는 메서드를 만들지 않는다**(잠금 도구는 잠긴 테스트가 쓰는 선택자 이름과 같은 이름의 같은 폴더 메서드를 모두 지문에 넣으므로, 포트를 흉내 내는 메서드는 기존 테스트의 잠금을 깬다).

## 바뀌는 의미

기대값은 아래 규칙으로 손으로 썼다. 옛 답은 이 변경 직전의 코드가 낸 것이다. 새 경로(뒤의 PR 4와 5)에만 닿는 변경은 다음 기록이 적는다.

| # | 입력 | 옛 답 | 새 답 | 경로 |
|---|---|---|---|---|
| 1 | 머리글 `Notes`가 `B1:D1`로 병합되고 아래 행에는 C, D열 칸이 없는 시트 | 머리글은 사용 범위의 끝 열(B)까지: `undeclared_column` 1건, 머리글 `Asset, Notes` | 병합의 끝 열(D)까지: 이름 `Notes`가 B, C, D열의 것이라 `undeclared_column` 3건, 머리글 목록은 그대로 `Asset, Notes` | 옛 `ParseSheet`(PR 1) |
| 2 | 머리글 칸이 `B1:XFD1`로 병합된 시트 | 사용 범위까지의 이름 | 이름은 1,024열까지(`undeclared_column` 1,023건). 그 글은 하나를 나눠 쓴다 | 옛 `ParseSheet`(PR 1) |
| 3 | 같은 번호의 행이 둘인 시트 | 두 행을 모두 읽되 둘 다 뒤 행의 칸으로 읽고 `blank_row`가 −1 | `Table.Reason` = `the workbook is damaged: the rows of sheet "S" are not in order (row 2 after row 2)`, 데이터셋은 `shape_changed` | 옛 `ParseSheet`(PR 1) |
| 4 | 행 순서가 틀리거나 한 행의 칸 순서가 틀린 `Sheet`(손으로 만든 것. 옛 `ReadWorkbook`은 정렬해서 건네므로 파일에서는 PR 2부터) | `Sorted()` 없이 읽으면 잘못 읽힘 | 같은 `Table.Reason` | 옛 `ParseSheet`(PR 1), 행 파서(PR 2) |
| 5 | 머리글과 병합의 첫 칸의 글이 8 MiB를 넘는 시트 | 읽힘(행 수에 비례하는 메모리) | `Table.Reason` = `the workbook is over the safety limit max_kept_bytes (8388608) by sheet "S": …` | 옛 `ParseSheet`(PR 1) |
| 6 | 읽지 않는 열의 칸이 공백뿐이거나 값 없는 수식이고 그 행에 다른 값이 없다 | 빈 행 | 같음(`Cell.Blank`가 같은 판단을 싣는다) | 두 경로 |

`Sheet.Sorted()`, `Table`, `ParseSheet`의 서명과 `SheetOptions`의 모양은 그대로이다. 잠긴 테스트는 바뀌는 것이 없다(`amends_tests`는 없다. 확인은 아래 "결과").

## 이유

- **의미의 구현이 하나여야 두 경로가 갈라지지 않는다**(ADR-0006). 새 흐름이 스캐너를 쓰고 옛 `ParseSheet`가 같은 스캐너 위에 서면 머리글 이름과 행 순서는 둘이 함께 바뀐다. 그래서 바뀌는 의미를 숨기지 않고 표로 적고 기대값 파일에서 따로 둔다.
- **머리글 이름의 규칙은 흘려 읽을 수 있는 것 중 가장 옛 뜻에 가깝다.** 사용 범위로 자르는 옛 규칙은 아래쪽 행에 값이 있느냐에 따라 같은 머리글이 다른 이름 수를 냈다(검토가 확인: 같은 병합 머리글에서 `Extra`가 1개 또는 3개). 병합 범위의 끝 열은 머리글 행에서 정해진다.
- **상한과 나눠 쓰기는 이 규칙이 만든 구멍이라 이 PR에서 막는다.** 규칙이 명령줄의 옛 경로에도 들어가므로 뒤의 PR을 기다릴 수 없다.
- **빈칸 판단은 글과 따로 실린다.** 수식을 아래로 미리 채운 서식 파일이 흔한 실제 입력인데, 계산된 적 없는 수식은 읽지 않는 열에서 행이 아니라 빈칸이다. 글을 만들지 않는 읽기가 이를 `Skipped`(값 있음)로만 알리면 `unreadable_line`이 쏟아진다.

## 대안과 버린 이유

- **머리글 이름을 사용 범위까지 그대로 두기**: 시트의 끝을 보기 전에 머리글을 정할 수 없어 흘려 읽을 수 없다.
- **병합을 위한 읽기를 토큰으로 건너뛰기, 풀린 시트를 임시 파일에 쓰고 꼬리부터 읽기, 병합을 빈 칸의 모양으로 추측하기**: 각각 시간이 80% 늘고, 임시 파일의 정리와 비밀이 문제이고, 틀린 값을 조용히 만든다.
- **스캐너에 포트를 만들지 않고 `Table`로 모으기**: 시트 전체를 쥐게 되어 메모리가 행 수에 비례한다.
- **병합으로 이름을 받는 열을 올릴 수 있는 한도(`max_columns_per_sheet`)로 자르기**: 소스 정의가 올리면 목표가 깨진다. 이 목적에서는 고정한다.
- **공유 문자열을 `[]string`으로 쥐기**: 문자열 머리 16 바이트와 할당 단위의 여분이 더해져 긴 글에서 실제 힙이 셈을 넘는다(길이 32 KiB 위에서 1.25배).

## 결과

- 이 PR: `core.ingest`에 `SheetScanner`와 `SheetHead`와 `SheetLayout`과 `Cell.Skipped`·`Cell.Blank`, `ParseSheet`가 스캐너 위로, `Inspect`의 머리글 이름이 같은 `headerName`으로. 요구사항 REQ-800~804. 안내서의 문장 둘과 수집 설계의 구현 문장 하나.
- 뒤의 PR 2: 행 파서(`rowparser.go`)를 `sheet.go`에서 옮기고 `ReadWorkbook`을 그 위로(순서와 같은 번호의 거절), 공통 XML 감싸개와 목록 한도를 옛 경로에도, `Inspect` 격자의 병합 복사를 고친다. 뒤의 PR 3: 결정 1, 2, 10, 11, 12와 T7의 나머지 입력, 규모 테스트.
- 스캐너를 읽는 쪽(뒤의 PR 3)이 알아야 할 것: 스캐너가 `fill`로 병합의 값을 줄 때 병합의 첫 칸이 읽지 않는 열에 있으면 그 칸의 글이 필요하므로 읽는 쪽은 병합 첫 칸의 열도 글을 만들어야 한다(`Skipped`로 낸 첫 칸은 글이 비어 있다).
- 잠긴 테스트는 바뀌지 않는다. `pnpm lock-tests`로 새 테스트가 추가만 되고 기존 항목이 하나도 바뀌지 않는지(`lockcmp`의 CHANGED 0) 확인했다.
