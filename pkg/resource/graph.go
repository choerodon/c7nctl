package resource

import "fmt"

/**
 * 有向无环图，使用 kana 算法实现它的拓扑排序
 */
type Graph struct {
	Vertex int
	Adj    map[*Release][]*Release
}

func (g *Graph) AddVertex(r *Release) {
	if g.Adj == nil {
		g.Adj = make(map[*Release][]*Release)
	}
	// 顶点不存在时插入
	if len(g.Adj[r]) == 0 {
		g.Adj[r] = []*Release{}
	}
	g.Vertex = len(g.Adj)
}
func (g *Graph) AddEdges(from, to *Release) {
	if g.Adj == nil {
		g.Adj = make(map[*Release][]*Release)
	}
	g.Adj[from] = append(g.Adj[from], to)
}

func (g *Graph) String() {
	s := "start"
	for key, _ := range g.Adj {
		s += " -> " + key.String()
	}
	s += "\n"
	fmt.Println(s)
}

func (g *Graph) TopoSortByKahn() *QueueRelease {
	// 统计每个顶点的入度
	inDegree := make(map[*Release]int)

	for key, values := range g.Adj {
		// 在 inDegree 中初始化顶点
		if inDegree[key] == 0 {
			inDegree[key] = 0
		}
		for i := 0; i < len(values); i++ {
			inDegree[values[i]]++
		}
	}
	queue := new(QueueRelease)
	result := new(QueueRelease)
	// 将入度为零的顶点加入队列
	for key, value := range inDegree {
		if value == 0 {
			queue.Enqueue(key)
		}
	}
	for !queue.IsEmpty() {
		rls := queue.Dequeue()
		result.Enqueue(rls)
		for _, value := range g.Adj[rls] {
			inDegree[value]--
			if inDegree[value] == 0 {
				queue.Enqueue(value)
			}
		}
	}
	return result
}

func NewReleaseGraph(rls []*Release) *Graph {
	var graph = Graph{}

	for _, r := range rls {
		graph.AddVertex(r)
	}
	for _, r := range rls {
		for _, rName := range r.Requirements {
			if requirements := checkRequirements(rName, rls); requirements != nil {
				graph.AddEdges(requirements, r)
			}
		}
	}

	return &graph
}

func checkRequirements(rName string, rls []*Release) *Release {
	for _, r := range rls {
		if rName == r.Name {
			return r
		}
	}
	return nil
}
