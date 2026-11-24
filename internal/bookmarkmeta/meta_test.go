package bookmarkmeta
import("path/filepath";"testing";"time")
func TestPersist(t *testing.T){p:=filepath.Join(t.TempDir(),"details.json");s,e:=Open(p);if e!=nil{t.Fatal(e)};if e=s.Set("Engine",Record{Pron:"/ˈendʒɪn/",Meaning:"发动机",SavedAt:time.Now()});e!=nil{t.Fatal(e)};s,e=Open(p);if e!=nil{t.Fatal(e)};r,ok:=s.Get("engine");if !ok||r.Meaning!="发动机"{t.Fatal(r,ok)};if e=s.Remove("engine");e!=nil{t.Fatal(e)};if _,ok=s.Get("engine");ok{t.Fatal("remove")}}
