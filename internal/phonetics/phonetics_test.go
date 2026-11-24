package phonetics
import("context";"fmt";"net/http";"net/http/httptest";"testing")
func TestIPA(t *testing.T){s:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){if r.URL.Path!="/hello"{t.Fatal(r.URL.Path)};fmt.Fprint(w,`[{"phonetic":"/həˈləʊ/"}]`)}));defer s.Close();ipa,e:=(Client{BaseURL:s.URL}).Get(context.Background(),"hello");if e!=nil||ipa!="/həˈləʊ/"{t.Fatal(ipa,e)}}
