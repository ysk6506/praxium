/** Strongly connected components (Tarjan) of an import graph: each component with >1 file is a cycle. */
export function findCycles(graph: ReadonlyMap<string, ReadonlySet<string>>): string[][] {
  let index = 0;
  const stack: string[] = [];
  const onStack = new Set<string>();
  const indices = new Map<string, number>();
  const low = new Map<string, number>();
  const cycles: string[][] = [];

  const visit = (node: string): void => {
    indices.set(node, index);
    low.set(node, index);
    index++;
    stack.push(node);
    onStack.add(node);
    for (const next of [...(graph.get(node) ?? [])].sort()) {
      if (!graph.has(next)) continue;
      if (!indices.has(next)) {
        visit(next);
        low.set(node, Math.min(low.get(node) as number, low.get(next) as number));
      } else if (onStack.has(next)) {
        low.set(node, Math.min(low.get(node) as number, indices.get(next) as number));
      }
    }
    if (low.get(node) === indices.get(node)) {
      const component: string[] = [];
      for (;;) {
        const member = stack.pop() as string;
        onStack.delete(member);
        component.push(member);
        if (member === node) break;
      }
      if (component.length > 1 || graph.get(node)?.has(node)) cycles.push(component.sort());
    }
  };

  for (const node of [...graph.keys()].sort()) {
    if (!indices.has(node)) visit(node);
  }
  return cycles;
}
