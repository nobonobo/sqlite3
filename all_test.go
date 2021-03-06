// Copyright 2017 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "modernc.org/sqlite"

import (
	"bytes"
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"modernc.org/libc"
	"modernc.org/mathutil"
)

func caller(s string, va ...interface{}) {
	if s == "" {
		s = strings.Repeat("%v ", len(va))
	}
	_, fn, fl, _ := runtime.Caller(2)
	fmt.Fprintf(os.Stderr, "# caller: %s:%d: ", path.Base(fn), fl)
	fmt.Fprintf(os.Stderr, s, va...)
	fmt.Fprintln(os.Stderr)
	_, fn, fl, _ = runtime.Caller(1)
	fmt.Fprintf(os.Stderr, "# \tcallee: %s:%d: ", path.Base(fn), fl)
	fmt.Fprintln(os.Stderr)
	os.Stderr.Sync()
}

func dbg(s string, va ...interface{}) {
	if s == "" {
		s = strings.Repeat("%v ", len(va))
	}
	_, fn, fl, _ := runtime.Caller(1)
	fmt.Fprintf(os.Stderr, "# dbg %s:%d: ", path.Base(fn), fl)
	fmt.Fprintf(os.Stderr, s, va...)
	fmt.Fprintln(os.Stderr)
	os.Stderr.Sync()
}

func stack() string { return string(debug.Stack()) }

func use(...interface{}) {}

func init() {
	use(caller, dbg, stack, todo, trc) //TODOOK
}

func origin(skip int) string {
	pc, fn, fl, _ := runtime.Caller(skip)
	f := runtime.FuncForPC(pc)
	var fns string
	if f != nil {
		fns = f.Name()
		if x := strings.LastIndex(fns, "."); x > 0 {
			fns = fns[x+1:]
		}
	}
	return fmt.Sprintf("%s:%d:%s", fn, fl, fns)
}

func todo(s string, args ...interface{}) string { //TODO-
	switch {
	case s == "":
		s = fmt.Sprintf(strings.Repeat("%v ", len(args)), args...)
	default:
		s = fmt.Sprintf(s, args...)
	}
	r := fmt.Sprintf("%s: TODOTODO %s", origin(2), s) //TODOOK
	fmt.Fprintf(os.Stdout, "%s\n", r)
	os.Stdout.Sync()
	return r
}

func trc(s string, args ...interface{}) string { //TODO-
	switch {
	case s == "":
		s = fmt.Sprintf(strings.Repeat("%v ", len(args)), args...)
	default:
		s = fmt.Sprintf(s, args...)
	}
	r := fmt.Sprintf("\n%s: TRC %s", origin(2), s)
	fmt.Fprintf(os.Stdout, "%s\n", r)
	os.Stdout.Sync()
	return r
}

// ============================================================================

var (
	oRecsPerSec = flag.Bool("recs_per_sec_as_mbps", false, "Show records per second as MB/s.")
	oXTags      = flag.String("xtags", "", "passed to go build of testfixture in TestTclTest")
	tempDir     string
)

func TestMain(m *testing.M) {
	fmt.Printf("test binary compiled for %s/%s\n", runtime.GOOS, runtime.GOARCH)
	flag.Parse()
	libc.MemAuditStart()
	os.Exit(testMain(m))
}

func testMain(m *testing.M) int {
	var err error
	tempDir, err = ioutil.TempDir("", "sqlite-test-")
	if err != nil {
		panic(err) //TODOOK
	}

	defer os.RemoveAll(tempDir)

	return m.Run()
}

func tempDB(t testing.TB) (string, *sql.DB) {
	dir, err := ioutil.TempDir("", "sqlite-test-")
	if err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open(driverName, filepath.Join(dir, "tmp.db"))
	if err != nil {
		os.RemoveAll(dir)
		t.Fatal(err)
	}

	return dir, db
}

func TestScalar(t *testing.T) {
	dir, db := tempDB(t)

	defer func() {
		db.Close()
		os.RemoveAll(dir)
	}()

	t1 := time.Date(2017, 4, 20, 1, 2, 3, 56789, time.UTC)
	t2 := time.Date(2018, 5, 21, 2, 3, 4, 98765, time.UTC)
	r, err := db.Exec(`
	create table t(i int, f double, b bool, s text, t time);
	insert into t values(12, 3.14, ?, 'foo', ?), (34, 2.78, ?, 'bar', ?);
	`,
		true, t1,
		false, t2,
	)
	if err != nil {
		t.Fatal(err)
	}

	n, err := r.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}

	if g, e := n, int64(2); g != e {
		t.Fatal(g, e)
	}

	rows, err := db.Query("select * from t")
	if err != nil {
		t.Fatal(err)
	}

	type rec struct {
		i int
		f float64
		b bool
		s string
		t string
	}
	var a []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.i, &r.f, &r.b, &r.s, &r.t); err != nil {
			t.Fatal(err)
		}

		a = append(a, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if g, e := len(a), 2; g != e {
		t.Fatal(g, e)
	}

	if g, e := a[0], (rec{12, 3.14, true, "foo", t1.String()}); g != e {
		t.Fatal(g, e)
	}

	if g, e := a[1], (rec{34, 2.78, false, "bar", t2.String()}); g != e {
		t.Fatal(g, e)
	}
}

func TestBlob(t *testing.T) {
	dir, db := tempDB(t)

	defer func() {
		db.Close()
		os.RemoveAll(dir)
	}()

	b1 := []byte(time.Now().String())
	b2 := []byte("\x00foo\x00bar\x00")
	if _, err := db.Exec(`
	create table t(b blob);
	insert into t values(?), (?);
	`, b1, b2,
	); err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query("select * from t")
	if err != nil {
		t.Fatal(err)
	}

	type rec struct {
		b []byte
	}
	var a []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.b); err != nil {
			t.Fatal(err)
		}

		a = append(a, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if g, e := len(a), 2; g != e {
		t.Fatal(g, e)
	}

	if g, e := a[0].b, b1; !bytes.Equal(g, e) {
		t.Fatal(g, e)
	}

	if g, e := a[1].b, b2; !bytes.Equal(g, e) {
		t.Fatal(g, e)
	}
}

func benchmarkInsertMemory(b *testing.B, n int) {
	db, err := sql.Open(driverName, "file::memory:")
	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		db.Close()
	}()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if _, err := db.Exec(`
		drop table if exists t;
		create table t(i int);
		begin;
		`); err != nil {
			b.Fatal(err)
		}

		s, err := db.Prepare("insert into t values(?)")
		if err != nil {
			b.Fatal(err)
		}

		b.StartTimer()
		for i := 0; i < n; i++ {
			if _, err := s.Exec(int64(i)); err != nil {
				b.Fatal(err)
			}
		}
		b.StopTimer()
		if _, err := db.Exec(`commit;`); err != nil {
			b.Fatal(err)
		}
	}
	if *oRecsPerSec {
		b.SetBytes(1e6 * int64(n))
	}
}

func BenchmarkInsertMemory(b *testing.B) {
	for i, n := range []int{1e1, 1e2, 1e3, 1e4, 1e5, 1e6} {
		b.Run(fmt.Sprintf("1e%d", i+1), func(b *testing.B) { benchmarkInsertMemory(b, n) })
	}
}

var staticInt int

func benchmarkNextMemory(b *testing.B, n int) {
	db, err := sql.Open(driverName, "file::memory:")
	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		db.Close()
	}()

	if _, err := db.Exec(`
		create table t(i int);
		begin;
		`); err != nil {
		b.Fatal(err)
	}

	s, err := db.Prepare("insert into t values(?)")
	if err != nil {
		b.Fatal(err)
	}

	for i := 0; i < n; i++ {
		if _, err := s.Exec(int64(i)); err != nil {
			b.Fatal(err)
		}
	}
	if _, err := db.Exec(`commit;`); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		r, err := db.Query("select * from t")
		if err != nil {
			b.Fatal(err)
		}

		b.StartTimer()
		for i := 0; i < n; i++ {
			if !r.Next() {
				b.Fatal(err)
			}
			if err := r.Scan(&staticInt); err != nil {
				b.Fatal(err)
			}
		}
		b.StopTimer()
		if err := r.Err(); err != nil {
			b.Fatal(err)
		}

		r.Close()
	}
	if *oRecsPerSec {
		b.SetBytes(1e6 * int64(n))
	}
}

func BenchmarkNextMemory(b *testing.B) {
	for i, n := range []int{1e1, 1e2, 1e3, 1e4, 1e5, 1e6} {
		b.Run(fmt.Sprintf("1e%d", i+1), func(b *testing.B) { benchmarkNextMemory(b, n) })
	}
}

// https://gitlab.com/cznic/sqlite/issues/11
func TestIssue11(t *testing.T) {
	const N = 6570
	dir, db := tempDB(t)

	defer func() {
		db.Close()
		os.RemoveAll(dir)
	}()

	if _, err := db.Exec(`
	CREATE TABLE t1 (t INT);
	BEGIN;
`,
	); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < N; i++ {
		if _, err := db.Exec("INSERT INTO t1 (t) VALUES (?)", i); err != nil {
			t.Fatalf("#%v: %v", i, err)
		}
	}
	if _, err := db.Exec("COMMIT;"); err != nil {
		t.Fatal(err)
	}
}

// https://gitlab.com/cznic/sqlite/issues/12
func TestMemDB(t *testing.T) {
	// Verify we can create out-of-the heap memory DB instance.
	db, err := sql.Open(driverName, "file::memory:")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		db.Close()
	}()

	v := strings.Repeat("a", 1024)
	if _, err := db.Exec(`
	create table t(s string);
	begin;
	`); err != nil {
		t.Fatal(err)
	}

	s, err := db.Prepare("insert into t values(?)")
	if err != nil {
		t.Fatal(err)
	}

	// Heap used to be fixed at 32MB.
	for i := 0; i < (64<<20)/len(v); i++ {
		if _, err := s.Exec(v); err != nil {
			t.Fatalf("%v * %v= %v: %v", i, len(v), i*len(v), err)
		}
	}
	if _, err := db.Exec(`commit;`); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentGoroutines(t *testing.T) {
	const (
		ngoroutines = 8
		nrows       = 5000
	)

	dir, err := ioutil.TempDir("", "sqlite-test-")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		os.RemoveAll(dir)
	}()

	db, err := sql.Open(driverName, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}

	defer db.Close()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tx.Exec("create table t(i)"); err != nil {
		t.Fatal(err)
	}

	prep, err := tx.Prepare("insert into t values(?)")
	if err != nil {
		t.Fatal(err)
	}

	rnd := make(chan int, 100)
	go func() {
		lim := ngoroutines * nrows
		rng, err := mathutil.NewFC32(0, lim-1, false)
		if err != nil {
			panic(fmt.Errorf("internal error: %v", err))
		}

		for i := 0; i < lim; i++ {
			rnd <- rng.Next()
		}
	}()

	start := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < ngoroutines; i++ {
		wg.Add(1)

		go func(id int) {

			defer wg.Done()

		next:
			for i := 0; i < nrows; i++ {
				n := <-rnd
				var err error
				for j := 0; j < 10; j++ {
					if _, err := prep.Exec(n); err == nil {
						continue next
					}
				}

				t.Errorf("id %d, seq %d: %v", id, i, err)
				return
			}
		}(i)
	}
	t0 := time.Now()
	close(start)
	wg.Wait()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	d := time.Since(t0)
	rows, err := db.Query("select * from t order by i")
	if err != nil {
		t.Fatal(err)
	}

	var i int
	for ; rows.Next(); i++ {
		var j int
		if err := rows.Scan(&j); err != nil {
			t.Fatalf("seq %d: %v", i, err)
		}

		if g, e := j, i; g != e {
			t.Fatalf("seq %d: got %d, exp %d", i, g, e)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if g, e := i, ngoroutines*nrows; g != e {
		t.Fatalf("got %d rows, expected %d", g, e)
	}

	t.Logf("%d goroutines concurrently inserted %d rows in %v", ngoroutines, ngoroutines*nrows, d)
}

func TestConcurrentProcesses(t *testing.T) {
	dir, err := ioutil.TempDir("", "sqlite-test-")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		os.RemoveAll(dir)
	}()

	m, err := filepath.Glob(filepath.FromSlash("internal/mptest/*"))
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range m {
		if s := filepath.Ext(v); s != ".test" && s != ".subtest" {
			continue
		}

		b, err := ioutil.ReadFile(v)
		if err != nil {
			t.Fatal(err)
		}

		if runtime.GOOS == "windows" {
			// reference tests are in *nix format --
			// but git on windows does line-ending xlation by default
			// if someone has it 'off' this has no impact.
			// '\r\n'  -->  '\n'
			b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
		}

		if err := ioutil.WriteFile(filepath.Join(dir, filepath.Base(v)), b, 0666); err != nil {
			t.Fatal(err)
		}
	}

	bin := "./mptest"
	if runtime.GOOS == "windows" {
		bin += "mptest.exe"
	}
	args := []string{"build", "-o", filepath.Join(dir, bin)}
	if s := *oXTags; s != "" {
		args = append(args, "-tags", s)
	}
	args = append(args, "modernc.org/sqlite/internal/mptest")
	out, err := exec.Command("go", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s\n%v", out, err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	defer os.Chdir(wd)

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

outer:
	for _, script := range m {
		script = filepath.Base(script)
		if filepath.Ext(script) != ".test" {
			continue
		}

		out, err := exec.Command(filepath.FromSlash(bin), "db", "--trace", "2", script).CombinedOutput()
		if err != nil {
			t.Fatalf("%s\n%v", out, err)
		}

		// just remove it so we don't get a
		// file busy race-condition
		// when we spin up the next script
		if runtime.GOOS == "windows" {
			_ = os.Remove("db")
		}

		a := strings.Split(string(out), "\n")
		for _, v := range a {
			if strings.HasPrefix(v, "Summary:") {
				b := strings.Fields(v)
				if len(b) < 2 {
					t.Fatalf("unexpected format of %q", v)
				}

				n, err := strconv.Atoi(b[1])
				if err != nil {
					t.Fatalf("unexpected format of %q", v)
				}

				if n != 0 {
					t.Errorf("%s", out)
				}

				t.Logf("%v: %v", script, v)
				continue outer
			}

		}
		t.Fatalf("%s\nerror: summary line not found", out)
	}
}

// https://gitlab.com/cznic/sqlite/issues/19
func TestIssue19(t *testing.T) {
	const (
		drop = `
drop table if exists products;
`

		up = `
CREATE TABLE IF NOT EXISTS "products" (
	"id"	VARCHAR(255),
	"user_id"	VARCHAR(255),
	"name"	VARCHAR(255),
	"description"	VARCHAR(255),
	"created_at"	BIGINT,
	"credits_price"	BIGINT,
	"enabled"	BOOLEAN,
	PRIMARY KEY("id")
);
`

		productInsert = `
INSERT INTO "products" ("id", "user_id", "name", "description", "created_at", "credits_price", "enabled") VALUES ('9be4398c-d527-4efb-93a4-fc532cbaf804', '16935690-348b-41a6-bb20-f8bb16011015', 'dqdwqdwqdwqwqdwqd', 'qwdwqwqdwqdwqdwqd', '1577448686', '1', '0');
INSERT INTO "products" ("id", "user_id", "name", "description", "created_at", "credits_price", "enabled") VALUES ('759f10bd-9e1d-4ec7-b764-0868758d7b85', '16935690-348b-41a6-bb20-f8bb16011015', 'qdqwqwdwqdwqdwqwqd', 'wqdwqdwqdwqdwqdwq', '1577448692', '1', '1');
INSERT INTO "products" ("id", "user_id", "name", "description", "created_at", "credits_price", "enabled") VALUES ('512956e7-224d-4b2a-9153-b83a52c4aa38', '16935690-348b-41a6-bb20-f8bb16011015', 'qwdwqwdqwdqdwqwqd', 'wqdwdqwqdwqdwqdwqdwqdqw', '1577448699', '2', '1');
INSERT INTO "products" ("id", "user_id", "name", "description", "created_at", "credits_price", "enabled") VALUES ('02cd138f-6fa6-4909-9db7-a9d0eca4a7b7', '16935690-348b-41a6-bb20-f8bb16011015', 'qdwqdwqdwqwqdwdq', 'wqddwqwqdwqdwdqwdqwq', '1577448706', '3', '1');
`
	)

	dir, err := ioutil.TempDir("", "sqlite-test-")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		os.RemoveAll(dir)
	}()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	defer os.Chdir(wd)

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", "test.db")
	if err != nil {
		t.Fatal("failed to connect database")
	}

	defer db.Close()

	db.SetMaxOpenConns(1)

	if _, err = db.Exec(drop); err != nil {
		t.Fatal(err)
	}

	if _, err = db.Exec(up); err != nil {
		t.Fatal(err)
	}

	if _, err = db.Exec(productInsert); err != nil {
		t.Fatal(err)
	}

	var count int64
	if err = db.QueryRow("select count(*) from products where user_id = ?", "16935690-348b-41a6-bb20-f8bb16011015").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 4 {
		t.Fatalf("expected result for the count query %d, we received %d\n", 4, count)
	}

	rows, err := db.Query("select * from products where user_id = ?", "16935690-348b-41a6-bb20-f8bb16011015")
	if err != nil {
		t.Fatal(err)
	}

	count = 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if count != 4 {
		t.Fatalf("expected result for the select query %d, we received %d\n", 4, count)
	}

	rows, err = db.Query("select * from products where enabled = ?", true)
	if err != nil {
		t.Fatal(err)
	}

	count = 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if count != 3 {
		t.Fatalf("expected result for the enabled select query %d, we received %d\n", 3, count)
	}
}

func mustExec(t *testing.T, db *sql.DB, sql string, args ...interface{}) sql.Result {
	res, err := db.Exec(sql, args...)
	if err != nil {
		t.Fatalf("Error running %q: %v", sql, err)
	}

	return res
}

// https://gitlab.com/cznic/sqlite/issues/20
func TestIssue20(t *testing.T) {
	const TablePrefix = "gosqltest_"

	tempDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		os.RemoveAll(tempDir)
	}()

	db, err := sql.Open("sqlite", filepath.Join(tempDir, "foo.db"))
	if err != nil {
		t.Fatalf("foo.db open fail: %v", err)
	}

	defer db.Close()

	mustExec(t, db, "CREATE TABLE "+TablePrefix+"t (count INT)")
	sel, err := db.PrepareContext(context.Background(), "SELECT count FROM "+TablePrefix+"t ORDER BY count DESC")
	if err != nil {
		t.Fatalf("prepare 1: %v", err)
	}

	ins, err := db.PrepareContext(context.Background(), "INSERT INTO "+TablePrefix+"t (count) VALUES (?)")
	if err != nil {
		t.Fatalf("prepare 2: %v", err)
	}

	for n := 1; n <= 3; n++ {
		if _, err := ins.Exec(n); err != nil {
			t.Fatalf("insert(%d) = %v", n, err)
		}
	}

	const nRuns = 10
	ch := make(chan bool)
	for i := 0; i < nRuns; i++ {
		go func() {
			defer func() {
				ch <- true
			}()
			for j := 0; j < 10; j++ {
				count := 0
				if err := sel.QueryRow().Scan(&count); err != nil && err != sql.ErrNoRows {
					t.Errorf("Query: %v", err)
					return
				}

				if _, err := ins.Exec(rand.Intn(100)); err != nil {
					t.Errorf("Insert: %v", err)
					return
				}
			}
		}()
	}
	for i := 0; i < nRuns; i++ {
		<-ch
	}
}

func TestNoRows(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		os.RemoveAll(tempDir)
	}()

	db, err := sql.Open("sqlite", filepath.Join(tempDir, "foo.db"))
	if err != nil {
		t.Fatalf("foo.db open fail: %v", err)
	}

	defer func() {
		db.Close()
	}()

	stmt, err := db.Prepare("create table t(i);")
	if err != nil {
		t.Fatal(err)
	}

	defer stmt.Close()

	if _, err := stmt.Query(); err != nil {
		t.Fatal(err)
	}
}

func TestColumns(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec("create table t1(a integer, b text, c blob)"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec("insert into t1 (a) values (1)"); err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query("select * from t1")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	got, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got columns %v, want %v", got, want)
	}
}

// https://gitlab.com/cznic/sqlite/-/issues/32
func TestColumnsNoRows(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec("create table t1(a integer, b text, c blob)"); err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query("select * from t1")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	got, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got columns %v, want %v", got, want)
	}
}

// https://gitlab.com/cznic/sqlite/-/issues/28
func TestIssue28(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		os.RemoveAll(tempDir)
	}()

	db, err := sql.Open("sqlite", filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("test.db open fail: %v", err)
	}

	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE test (foo TEXT)`); err != nil {
		t.Fatal(err)
	}

	row := db.QueryRow(`SELECT foo FROM test`)
	var foo string
	if err = row.Scan(&foo); err != sql.ErrNoRows {
		t.Fatalf("got %T(%[1]v), expected %T(%[2]v)", err, sql.ErrNoRows)
	}
}

// https://gitlab.com/cznic/sqlite/-/issues/30
func TestColumnTypes(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		os.RemoveAll(tempDir)
	}()

	db, err := sql.Open("sqlite", filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("test.db open fail: %v", err)
	}

	defer db.Close()

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS `userinfo` (`uid` INTEGER PRIMARY KEY AUTOINCREMENT,`username` VARCHAR(64) NULL, `departname` VARCHAR(64) NULL, `created` DATE NULL);")
	if err != nil {
		t.Fatal(err)
	}

	insertStatement := `INSERT INTO userinfo(username, departname, created) values("astaxie", "????????????", "2012-12-09")`
	_, err = db.Query(insertStatement)
	if err != nil {
		t.Fatal(err)
	}

	rows2, err := db.Query("SELECT * FROM userinfo")
	if err != nil {
		t.Fatal(err)
	}
	defer rows2.Close()

	columnTypes, err := rows2.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}

	var b strings.Builder
	for index, value := range columnTypes {
		precision, scale, precisionOk := value.DecimalSize()
		length, lengthOk := value.Length()
		nullable, nullableOk := value.Nullable()
		fmt.Fprintf(&b, "Col %d: DatabaseTypeName %q, DecimalSize %v %v %v, Length %v %v, Name %q, Nullable %v %v, ScanType %q\n",
			index,
			value.DatabaseTypeName(),
			precision, scale, precisionOk,
			length, lengthOk,
			value.Name(),
			nullable, nullableOk,
			value.ScanType(),
		)
	}
	if err := rows2.Err(); err != nil {
		t.Fatal(err)
	}

	if g, e := b.String(), `Col 0: DatabaseTypeName "INTEGER", DecimalSize 0 0 false, Length 0 false, Name "uid", Nullable true true, ScanType "int64"
Col 1: DatabaseTypeName "VARCHAR(64)", DecimalSize 0 0 false, Length 9223372036854775807 true, Name "username", Nullable true true, ScanType "string"
Col 2: DatabaseTypeName "VARCHAR(64)", DecimalSize 0 0 false, Length 9223372036854775807 true, Name "departname", Nullable true true, ScanType "string"
Col 3: DatabaseTypeName "DATE", DecimalSize 0 0 false, Length 9223372036854775807 true, Name "created", Nullable true true, ScanType "string"
`; g != e {
		t.Fatalf("---- got\n%s\n----expected\n%s", g, e)
	}
	t.Log(b.String())
}

// https://gitlab.com/cznic/sqlite/-/issues/32
func TestColumnTypesNoRows(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		os.RemoveAll(tempDir)
	}()

	db, err := sql.Open("sqlite", filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("test.db open fail: %v", err)
	}

	defer db.Close()

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS `userinfo` (`uid` INTEGER PRIMARY KEY AUTOINCREMENT,`username` VARCHAR(64) NULL, `departname` VARCHAR(64) NULL, `created` DATE NULL);")
	if err != nil {
		t.Fatal(err)
	}

	rows2, err := db.Query("SELECT * FROM userinfo")
	if err != nil {
		t.Fatal(err)
	}
	defer rows2.Close()

	columnTypes, err := rows2.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}

	var b strings.Builder
	for index, value := range columnTypes {
		precision, scale, precisionOk := value.DecimalSize()
		length, lengthOk := value.Length()
		nullable, nullableOk := value.Nullable()
		fmt.Fprintf(&b, "Col %d: DatabaseTypeName %q, DecimalSize %v %v %v, Length %v %v, Name %q, Nullable %v %v, ScanType %q\n",
			index,
			value.DatabaseTypeName(),
			precision, scale, precisionOk,
			length, lengthOk,
			value.Name(),
			nullable, nullableOk,
			value.ScanType(),
		)
	}
	if err := rows2.Err(); err != nil {
		t.Fatal(err)
	}

	if g, e := b.String(), `Col 0: DatabaseTypeName "INTEGER", DecimalSize 0 0 false, Length 0 false, Name "uid", Nullable true true, ScanType %!q(<nil>)
Col 1: DatabaseTypeName "VARCHAR(64)", DecimalSize 0 0 false, Length 0 false, Name "username", Nullable true true, ScanType %!q(<nil>)
Col 2: DatabaseTypeName "VARCHAR(64)", DecimalSize 0 0 false, Length 0 false, Name "departname", Nullable true true, ScanType %!q(<nil>)
Col 3: DatabaseTypeName "DATE", DecimalSize 0 0 false, Length 0 false, Name "created", Nullable true true, ScanType %!q(<nil>)
`; g != e {
		t.Fatalf("---- got\n%s\n----expected\n%s", g, e)
	}
	t.Log(b.String())
}

// https://gitlab.com/cznic/sqlite/-/issues/35
func TestTime(t *testing.T) {
	types := []string{
		"DATE",
		"DATETIME",
		"Date",
		"DateTime",
		"TIMESTAMP",
		"TimeStamp",
		"date",
		"datetime",
		"timestamp",
	}
	db, err := sql.Open(driverName, "file::memory:")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		db.Close()
	}()

	for _, typ := range types {
		if _, err := db.Exec(fmt.Sprintf(`
		drop table if exists mg;
		create table mg (applied_at %s);
		`, typ)); err != nil {
			t.Fatal(err)
		}

		now := time.Now()
		_, err = db.Exec(`INSERT INTO mg (applied_at) VALUES (?)`, &now)
		if err != nil {
			t.Fatal(err)
		}

		var appliedAt time.Time
		err = db.QueryRow("SELECT applied_at FROM mg").Scan(&appliedAt)
		if err != nil {
			t.Fatal(err)
		}

		if g, e := appliedAt, now; !g.Equal(e) {
			t.Fatal(g, e)
		}
	}
}

// https://gitlab.com/cznic/sqlite/-/issues/46
func TestTimeScan(t *testing.T) {
	ref := time.Date(2021, 1, 2, 16, 39, 17, 123456789, time.UTC)

	cases := []struct {
		s string
		w time.Time
	}{
		{s: "2021-01-02 12:39:17 -0400 ADT m=+00000", w: ref.Truncate(time.Second)},
		{s: "2021-01-02 16:39:17 +0000 UTC m=+0.000000001", w: ref.Truncate(time.Second)},
		{s: "2021-01-02 12:39:17.123456 -0400 ADT m=+00000", w: ref.Truncate(time.Microsecond)},
		{s: "2021-01-02 16:39:17.123456 +0000 UTC m=+0.000000001", w: ref.Truncate(time.Microsecond)},
		{s: "2021-01-02 16:39:17Z", w: ref.Truncate(time.Second)},
		{s: "2021-01-02 16:39:17+00:00", w: ref.Truncate(time.Second)},
		{s: "2021-01-02T16:39:17.123456+00:00", w: ref.Truncate(time.Microsecond)},
		{s: "2021-01-02 16:39:17.123456+00:00", w: ref.Truncate(time.Microsecond)},
		{s: "2021-01-02 12:39:17-04:00", w: ref.Truncate(time.Second)},
		{s: "2021-01-02 16:39:17", w: ref.Truncate(time.Second)},
		{s: "2021-01-02T16:39:17", w: ref.Truncate(time.Second)},
		{s: "2021-01-02 16:39", w: ref.Truncate(time.Minute)},
		{s: "2021-01-02T16:39", w: ref.Truncate(time.Minute)},
		{s: "2021-01-02", w: ref.Truncate(24 * time.Hour)},
	}

	db, err := sql.Open(driverName, "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, colType := range []string{"DATE", "DATETIME", "TIMESTAMP"} {
		for _, tc := range cases {
			if _, err := db.Exec("drop table if exists x; create table x (y " + colType + ")"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("insert into x (y) values (?)", tc.s); err != nil {
				t.Fatal(err)
			}

			var got time.Time

			if err := db.QueryRow("select y from x").Scan(&got); err != nil {
				t.Fatal(err)
			}
			if !got.Equal(tc.w) {
				t.Errorf("scan(%q as %q) = %s, want %s", tc.s, colType, got, tc.w)
			}
		}
	}
}

// https://gitlab.com/cznic/sqlite/-/issues/49
func TestTimeLocaltime(t *testing.T) {
	db, err := sql.Open(driverName, "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec("select datetime('now', 'localtime')"); err != nil {
		t.Fatal(err)
	}
}

// https://sqlite.org/lang_expr.html#varparam
// https://gitlab.com/cznic/sqlite/-/issues/42
func TestBinding(t *testing.T) {
	t.Run("DB", func(t *testing.T) {
		testBinding(t, func(db *sql.DB, query string, args ...interface{}) (*sql.Row, func()) {
			return db.QueryRow(query, args...), func() {}
		})
	})

	t.Run("Prepare", func(t *testing.T) {
		testBinding(t, func(db *sql.DB, query string, args ...interface{}) (*sql.Row, func()) {
			stmt, err := db.Prepare(query)
			if err != nil {
				t.Fatal(err)
			}
			return stmt.QueryRow(args...), func() { stmt.Close() }
		})
	})
}

func testBinding(t *testing.T, query func(db *sql.DB, query string, args ...interface{}) (*sql.Row, func())) {
	db, err := sql.Open(driverName, "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, tc := range []struct {
		q  string
		in []interface{}
		w  []int
	}{
		{
			q:  "?, ?, ?",
			in: []interface{}{1, 2, 3},
			w:  []int{1, 2, 3},
		},
		{
			q:  "?1, ?2, ?3",
			in: []interface{}{1, 2, 3},
			w:  []int{1, 2, 3},
		},
		{
			q:  "?1, ?, ?3",
			in: []interface{}{1, 2, 3},
			w:  []int{1, 2, 3},
		},
		{
			q:  "?3, ?2, ?1",
			in: []interface{}{1, 2, 3},
			w:  []int{3, 2, 1},
		},
		{
			q:  "?1, ?1, ?2",
			in: []interface{}{1, 2},
			w:  []int{1, 1, 2},
		},
		{
			q:  ":one, :two, :three",
			in: []interface{}{sql.Named("one", 1), sql.Named("two", 2), sql.Named("three", 3)},
			w:  []int{1, 2, 3},
		},
		{
			q:  ":one, :one, :two",
			in: []interface{}{sql.Named("one", 1), sql.Named("two", 2)},
			w:  []int{1, 1, 2},
		},
		{
			q:  "@one, @two, @three",
			in: []interface{}{sql.Named("one", 1), sql.Named("two", 2), sql.Named("three", 3)},
			w:  []int{1, 2, 3},
		},
		{
			q:  "@one, @one, @two",
			in: []interface{}{sql.Named("one", 1), sql.Named("two", 2)},
			w:  []int{1, 1, 2},
		},
		{
			q:  "$one, $two, $three",
			in: []interface{}{sql.Named("one", 1), sql.Named("two", 2), sql.Named("three", 3)},
			w:  []int{1, 2, 3},
		},
		{
			// A common usage that should technically require sql.Named but
			// does not.
			q:  "$1, $2, $3",
			in: []interface{}{1, 2, 3},
			w:  []int{1, 2, 3},
		},
		{
			q:  "$one, $one, $two",
			in: []interface{}{sql.Named("one", 1), sql.Named("two", 2)},
			w:  []int{1, 1, 2},
		},
		{
			q:  ":one, @one, $one",
			in: []interface{}{sql.Named("one", 1)},
			w:  []int{1, 1, 1},
		},
	} {
		got := make([]int, len(tc.w))
		ptrs := make([]interface{}, len(got))
		for i := range got {
			ptrs[i] = &got[i]
		}

		row, cleanup := query(db, "select "+tc.q, tc.in...)
		defer cleanup()

		if err := row.Scan(ptrs...); err != nil {
			t.Errorf("query(%q, %+v) = %s", tc.q, tc.in, err)
			continue
		}

		if !reflect.DeepEqual(got, tc.w) {
			t.Errorf("query(%q, %+v) = %#+v, want %#+v", tc.q, tc.in, got, tc.w)
		}
	}
}

func TestBindingError(t *testing.T) {
	t.Run("DB", func(t *testing.T) {
		testBindingError(t, func(db *sql.DB, query string, args ...interface{}) (*sql.Row, func()) {
			return db.QueryRow(query, args...), func() {}
		})
	})

	t.Run("Prepare", func(t *testing.T) {
		testBindingError(t, func(db *sql.DB, query string, args ...interface{}) (*sql.Row, func()) {
			stmt, err := db.Prepare(query)
			if err != nil {
				t.Fatal(err)
			}
			return stmt.QueryRow(args...), func() { stmt.Close() }
		})
	})
}

func testBindingError(t *testing.T, query func(db *sql.DB, query string, args ...interface{}) (*sql.Row, func())) {
	db, err := sql.Open(driverName, "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, tc := range []struct {
		q  string
		in []interface{}
	}{
		{
			q:  "?",
			in: []interface{}{},
		},
		{
			q:  "?500, ?",
			in: []interface{}{1, 2},
		},
		{
			q:  ":one",
			in: []interface{}{1},
		},
		{
			q:  "@one",
			in: []interface{}{1},
		},
		{
			q:  "$one",
			in: []interface{}{1},
		},
	} {
		got := make([]int, 2)
		ptrs := make([]interface{}, len(got))
		for i := range got {
			ptrs[i] = &got[i]
		}

		row, cleanup := query(db, "select "+tc.q, tc.in...)
		defer cleanup()

		err := row.Scan(ptrs...)
		if err == nil || (!strings.Contains(err.Error(), "missing argument with index") && !strings.Contains(err.Error(), "missing named argument")) {
			t.Errorf("query(%q, %+v) unexpected error %+v", tc.q, tc.in, err)
		}
	}
}

// https://gitlab.com/cznic/sqlite/-/issues/51
func TestIssue51(t *testing.T) {
	fn := filepath.Join(tempDir, "test_issue51.db")
	db, err := sql.Open(driverName, fn)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		db.Close()
	}()

	if _, err := db.Exec(`
CREATE TABLE fileHash (
	"hash" TEXT NOT NULL PRIMARY KEY,
	"filename" TEXT,
	"lastChecked" INTEGER
 );`); err != nil {
		t.Fatal(err)
	}

	t0 := time.Now()
	n := 0
	for time.Since(t0) < time.Minute {
		hash := randomString()
		if _, err = lookupHash(fn, hash); err != nil {
			t.Fatal(err)
		}

		if err = saveHash(fn, hash, hash+".temp"); err != nil {
			t.Error(err)
			break
		}
		n++
	}
	t.Logf("cycles: %v", n)
	row := db.QueryRow("select count(*) from fileHash")
	if err := row.Scan(&n); err != nil {
		t.Fatal(err)
	}

	t.Logf("DB records: %v", n)
}

func saveHash(dbFile string, hash string, fileName string) (err error) {
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		return fmt.Errorf("could not open database: %v", err)
	}

	defer func() {
		if err2 := db.Close(); err2 != nil && err == nil {
			err = fmt.Errorf("could not close the database: %s", err2)
		}
	}()

	query := `INSERT OR REPLACE INTO fileHash(hash, fileName, lastChecked)
			VALUES(?, ?, ?);`
	rows, err := executeSQL(db, query, hash, fileName, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("error saving hash to database: %v", err)
	}
	defer rows.Close()

	return nil
}

func executeSQL(db *sql.DB, query string, values ...interface{}) (*sql.Rows, error) {
	statement, err := db.Prepare(query)
	if err != nil {
		return nil, fmt.Errorf("could not prepare statement: %v", err)
	}
	defer statement.Close()

	return statement.Query(values...)
}

func lookupHash(dbFile string, hash string) (ok bool, err error) {
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		return false, fmt.Errorf("could not open database: %n", err)
	}

	defer func() {
		if err2 := db.Close(); err2 != nil && err == nil {
			err = fmt.Errorf("could not close the database: %v", err2)
		}
	}()

	query := `SELECT hash, fileName, lastChecked
				FROM fileHash
				WHERE hash=?;`
	rows, err := executeSQL(db, query, hash)
	if err != nil {
		return false, fmt.Errorf("error checking database for hash: %n", err)
	}

	defer func() {
		if err2 := rows.Close(); err2 != nil && err == nil {
			err = fmt.Errorf("could not close DB rows: %v", err2)
		}
	}()

	var (
		dbHash      string
		fileName    string
		lastChecked int64
	)
	for rows.Next() {
		err = rows.Scan(&dbHash, &fileName, &lastChecked)
		if err != nil {
			return false, fmt.Errorf("could not read DB row: %v", err)
		}
	}
	return false, rows.Err()
}

func randomString() string {
	b := make([]byte, 32)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

var seededRand *rand.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))

const charset = "abcdefghijklmnopqrstuvwxyz" +
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// https://gitlab.com/cznic/sqlite/-/issues/53
func TestIssue53(t *testing.T) {
	if err := emptyDir(tempDir); err != nil {
		t.Fatal(err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	defer os.Chdir(wd)

	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}

	os.Remove("x.sqlite")
	db, err := sql.Open(driverName, "x.sqlite")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS loginst (
     instid INTEGER PRIMARY KEY,
     name   VARCHAR UNIQUE
);
`); err != nil {
		t.Fatal(err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5000; i++ {
		x := fmt.Sprintf("foo%d", i)
		var id int
		if err := tx.QueryRow("INSERT OR IGNORE INTO loginst (name) VALUES (?); SELECT instid FROM loginst WHERE name = ?", x, x).Scan(&id); err != nil {
			t.Fatal(err)
		}
	}

}

func emptyDir(s string) error {
	m, err := filepath.Glob(filepath.FromSlash(s + "/*"))
	if err != nil {
		return err
	}

	for _, v := range m {
		fi, err := os.Stat(v)
		if err != nil {
			return err
		}

		switch {
		case fi.IsDir():
			if err = os.RemoveAll(v); err != nil {
				return err
			}
		default:
			if err = os.Remove(v); err != nil {
				return err
			}
		}
	}
	return nil
}
