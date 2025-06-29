package main

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"math/rand"
	"runtime"
	"time"

	"github.com/coder/hnsw"
	_ "modernc.org/sqlite"
)

/*
向量搜索 PoC - 技术选型与基准测试

选择的 HNSW 库：
1. github.com/coder/hnsw - 主要选择
   理由：
   - 纯 Go 实现，无 CGO 依赖
   - 专门实现了 HNSW 算法（层次可导航小世界）
   - 支持高维向量（768维）
   - API 简洁：Add()、Search()、SavedGraph() 用于持久化
   - 活跃维护，有实际产品使用（Coder）
   - 性能优秀：~797 MB/s 导入，~1232 MB/s 导出（M3 MacBook）

备选方案（如果 HNSW 不满足需求）：
2. github.com/rikonor/go-ann
   - 使用 MRPT（多重随机投影树）算法
   - 纯 Go 实现
   - 支持高维向量
   - 提供精确匹配的 naive 实现用于对比测试
*/

const (
	vectorDim   = 768    // 向量维度
	vectorCount = 10000  // 向量数量
	queryCount  = 100    // 查询次数
	topK        = 5      // Top-K 查询
)

// 生成随机向量
func generateRandomVectors(count, dim int) [][]float32 {
	vectors := make([][]float32, count)
	for i := 0; i < count; i++ {
		vec := make([]float32, dim)
		for j := 0; j < dim; j++ {
			vec[j] = rand.Float32()*2 - 1 // [-1, 1]
		}
		vectors[i] = vec
	}
	return vectors
}

// 将 float32 切片序列化为字节
func serializeVector(vec []float32) []byte {
	buf := new(bytes.Buffer)
	for _, v := range vec {
		binary.Write(buf, binary.LittleEndian, v)
	}
	return buf.Bytes()
}

// 从字节反序列化为 float32 切片
func deserializeVector(data []byte) []float32 {
	vec := make([]float32, len(data)/4)
	buf := bytes.NewReader(data)
	for i := range vec {
		binary.Read(buf, binary.LittleEndian, &vec[i])
	}
	return vec
}

// 计算两个向量的余弦相似度
func cosineSimilarity(a, b []float32) float32 {
	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

// 测试 1: SQLite 写入性能
func testSQLiteWrite(vectors [][]float32) time.Duration {
	fmt.Println("\n=== 测试 1: SQLite 写入性能 ===")
	
	db, err := sql.Open("sqlite", "test_vectors.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// 创建表
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS vectors (
			id INTEGER PRIMARY KEY,
			embedding BLOB
		)
	`)
	if err != nil {
		log.Fatal(err)
	}

	// 开始计时
	start := time.Now()

	// 使用事务批量插入
	tx, err := db.Begin()
	if err != nil {
		log.Fatal(err)
	}

	stmt, err := tx.Prepare("INSERT INTO vectors (id, embedding) VALUES (?, ?)")
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	for i, vec := range vectors {
		_, err = stmt.Exec(i, serializeVector(vec))
		if err != nil {
			log.Fatal(err)
		}
	}

	err = tx.Commit()
	if err != nil {
		log.Fatal(err)
	}

	duration := time.Since(start)
	fmt.Printf("写入 %d 个向量耗时: %v\n", len(vectors), duration)
	fmt.Printf("平均每个向量: %v\n", duration/time.Duration(len(vectors)))
	
	return duration
}

// 测试 2: HNSW 索引构建性能
func testHNSWIndexBuild(vectors [][]float32) (time.Duration, *hnsw.Graph[int], runtime.MemStats) {
	fmt.Println("\n=== 测试 2: HNSW 索引构建性能 ===")
	
	// 记录内存使用前
	var memBefore, memAfter runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	// 创建 HNSW 索引
	index := hnsw.NewGraph[int]()
	// 设置参数
	index.M = 16                      // 每个节点的最大邻居数
	index.EfSearch = 200              // 搜索时考虑的节点数
	index.Distance = hnsw.CosineDistance

	// 开始计时
	start := time.Now()

	// 构建索引
	for i, vec := range vectors {
		node := hnsw.MakeNode(i, vec)
		index.Add(node)
	}

	duration := time.Since(start)
	
	// 记录内存使用后
	runtime.ReadMemStats(&memAfter)
	
	memUsed := memAfter.Alloc - memBefore.Alloc
	fmt.Printf("构建 %d 个向量的索引耗时: %v\n", len(vectors), duration)
	fmt.Printf("平均每个向量: %v\n", duration/time.Duration(len(vectors)))
	fmt.Printf("内存使用: %.2f MB\n", float64(memUsed)/(1024*1024))
	
	return duration, index, memAfter
}

// 测试 3: 暴力搜索查询性能
func testBruteForceSearch(vectors [][]float32, queryVectors [][]float32) time.Duration {
	fmt.Println("\n=== 测试 3: 暴力搜索查询性能 ===")
	
	// 从 SQLite 读取向量
	db, err := sql.Open("sqlite", "test_vectors.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// 开始计时
	start := time.Now()
	
	for _, queryVec := range queryVectors {
		// 读取所有向量
		rows, err := db.Query("SELECT id, embedding FROM vectors")
		if err != nil {
			log.Fatal(err)
		}

		type result struct {
			id    int
			score float32
		}
		results := make([]result, 0, topK)

		for rows.Next() {
			var id int
			var embeddingBytes []byte
			err = rows.Scan(&id, &embeddingBytes)
			if err != nil {
				log.Fatal(err)
			}

			vec := deserializeVector(embeddingBytes)
			score := cosineSimilarity(queryVec, vec)

			// 维护 top-K
			if len(results) < topK {
				results = append(results, result{id: id, score: score})
			} else {
				// 找到最小分数
				minIdx := 0
				for i := 1; i < len(results); i++ {
					if results[i].score < results[minIdx].score {
						minIdx = i
					}
				}
				if score > results[minIdx].score {
					results[minIdx] = result{id: id, score: score}
				}
			}
		}
		rows.Close()
	}

	duration := time.Since(start)
	avgDuration := duration / time.Duration(len(queryVectors))
	
	fmt.Printf("%d 次查询总耗时: %v\n", len(queryVectors), duration)
	fmt.Printf("平均每次查询: %v\n", avgDuration)
	
	return duration
}

// 测试 4: HNSW 索引查询性能
func testHNSWSearch(index *hnsw.Graph[int], queryVectors [][]float32) time.Duration {
	fmt.Println("\n=== 测试 4: HNSW 索引查询性能 ===")
	
	// 设置搜索参数
	index.EfSearch = 50 // ef 影响搜索质量和速度的平衡

	// 开始计时
	start := time.Now()
	
	for _, queryVec := range queryVectors {
		// 执行 HNSW 搜索
		_ = index.Search(queryVec, topK)
	}

	duration := time.Since(start)
	avgDuration := duration / time.Duration(len(queryVectors))
	
	fmt.Printf("%d 次查询总耗时: %v\n", len(queryVectors), duration)
	fmt.Printf("平均每次查询: %v\n", avgDuration)
	
	return duration
}

func main() {
	fmt.Println("PCAS SQLite 向量搜索 PoC 基准测试")
	fmt.Println("================================")
	fmt.Printf("向量维度: %d\n", vectorDim)
	fmt.Printf("向量数量: %d\n", vectorCount)
	fmt.Printf("查询次数: %d\n", queryCount)
	fmt.Printf("Top-K: %d\n", topK)

	// 生成测试数据
	fmt.Println("\n生成测试数据...")
	vectors := generateRandomVectors(vectorCount, vectorDim)
	queryVectors := generateRandomVectors(queryCount, vectorDim)
	fmt.Println("数据生成完成")

	// 执行测试
	sqliteWriteDuration := testSQLiteWrite(vectors)
	hnswBuildDuration, hnswIndex, memStats := testHNSWIndexBuild(vectors)
	bruteForceSearchDuration := testBruteForceSearch(vectors, queryVectors)
	hnswSearchDuration := testHNSWSearch(hnswIndex, queryVectors)

	// 输出总结报告
	fmt.Println("\n================== 性能测试报告 ==================")
	fmt.Printf("1. SQLite 写入 %d 个向量: %v\n", vectorCount, sqliteWriteDuration)
	fmt.Printf("   - 平均每个向量: %v\n", sqliteWriteDuration/time.Duration(vectorCount))
	fmt.Printf("   - 吞吐量: %.2f vectors/秒\n", float64(vectorCount)/sqliteWriteDuration.Seconds())
	
	fmt.Printf("\n2. HNSW 索引构建 %d 个向量: %v\n", vectorCount, hnswBuildDuration)
	fmt.Printf("   - 平均每个向量: %v\n", hnswBuildDuration/time.Duration(vectorCount))
	fmt.Printf("   - 内存使用: %.2f MB\n", float64(memStats.Alloc-memStats.HeapAlloc)/(1024*1024))
	fmt.Printf("   - 吞吐量: %.2f vectors/秒\n", float64(vectorCount)/hnswBuildDuration.Seconds())
	
	fmt.Printf("\n3. 暴力搜索 %d 次查询: %v\n", queryCount, bruteForceSearchDuration)
	fmt.Printf("   - 平均延迟: %v\n", bruteForceSearchDuration/time.Duration(queryCount))
	fmt.Printf("   - QPS: %.2f\n", float64(queryCount)/bruteForceSearchDuration.Seconds())
	
	fmt.Printf("\n4. HNSW 索引 %d 次查询: %v\n", queryCount, hnswSearchDuration)
	fmt.Printf("   - 平均延迟: %v\n", hnswSearchDuration/time.Duration(queryCount))
	fmt.Printf("   - QPS: %.2f\n", float64(queryCount)/hnswSearchDuration.Seconds())
	
	// 计算加速比
	speedup := float64(bruteForceSearchDuration) / float64(hnswSearchDuration)
	fmt.Printf("\n5. HNSW vs 暴力搜索加速比: %.2fx\n", speedup)
	fmt.Println("=================================================")
}