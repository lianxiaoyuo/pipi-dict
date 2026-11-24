package freeonline
import("context";"fmt";"net/http";"net/http/httptest";"strings";"sync/atomic";"testing")
func TestTranslateParagraph(t *testing.T){
 var reqs atomic.Int32
 s:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){reqs.Add(1);if len(r.URL.Query().Get("q"))>460{t.Errorf("oversize chunk")};fmt.Fprint(w,`{"responseStatus":200,"responseData":{"translatedText":"一段翻译"}}`)}));defer s.Close()
 translation,err:=(Client{Endpoint:s.URL}).TranslateText(context.Background(),strings.Repeat("This sentence is useful and substantial. ",28));if err!=nil{t.Fatal(err)};if reqs.Load()<2{t.Fatal("expected multiple chunks")};if !strings.Contains(translation,"一段翻译"){t.Fatal(translation)}
}
