import { connection, type Route, routeHash, useRoute, useToken } from "./data";
import { KgsScreen, ObjectScreen, ObjectsScreen, SettingsScreen, TypesScreen } from "./screens";
import { AppShell, Button, NavLink, NavSection } from "./ui";

function currentKg(route: Route): string | undefined {
  return "kg" in route ? route.kg : undefined;
}

function Screen({ route, go }: { readonly route: Route; readonly go: (route: Route) => void }) {
  switch (route.page) {
    case "settings":
      return <SettingsScreen />;
    case "kgs":
      return <KgsScreen />;
    case "types":
      return <TypesScreen kg={route.kg} />;
    case "objects":
      return <ObjectsScreen key={route.type} kg={route.kg} type={route.type} />;
    case "object":
      return (
        <ObjectScreen
          kg={route.kg}
          id={route.id}
          tab={route.tab}
          onTab={(tab) => go({ ...route, tab })}
        />
      );
  }
}

export function App() {
  const token = useToken();
  const { route, go } = useRoute(token !== "");
  const kg = currentKg(route);
  const nav = (
    <>
      <NavSection title="지식 그래프">
        <NavLink href={routeHash({ page: "kgs" })} current={route.page === "kgs"}>
          KG 선택
        </NavLink>
        {kg ? (
          <NavLink
            href={routeHash({ page: "types", kg })}
            current={route.page === "types" || route.page === "objects"}
          >
            객체 타입 ({kg})
          </NavLink>
        ) : null}
      </NavSection>
      <NavSection title="설정">
        <NavLink href={routeHash({ page: "settings" })} current={route.page === "settings"}>
          접속 설정
        </NavLink>
      </NavSection>
    </>
  );
  return (
    <AppShell
      brand="프락시움"
      nav={nav}
      footer={
        token ? (
          <Button variant="ghost" onClick={() => connection.clear()}>
            토큰 삭제
          </Button>
        ) : (
          "토큰이 없습니다"
        )
      }
    >
      <Screen route={route} go={go} />
    </AppShell>
  );
}
