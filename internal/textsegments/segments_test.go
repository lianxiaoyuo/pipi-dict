package textsegments
import("strings";"testing")
func TestSplitBoundaries(t *testing.T){
 text:=strings.Repeat("This is a longer sentence with useful information. ",45)
 chunks,err:=Split(text);if err!=nil{t.Fatal(err)};if len(chunks)<3{t.Fatalf("want chunks got %d",len(chunks))}
 for _, c:=range chunks {if len([]byte(c))>RequestBytes {t.Fatalf("oversize %d",len(c))}}
 if strings.Join(chunks," ")!=strings.TrimSpace(text){t.Fatal("content lost")}
}
func TestTooLong(t *testing.T){_,err:=Split(strings.Repeat("word ",700));if err==nil{t.Fatal("must reject oversized text")}}
func TestChineseText(t *testing.T){chunks,err:=Split(strings.Repeat("安全翻译中文句子。",100));if err!=nil{t.Fatal(err)};for _,c:=range chunks{if len(c)>RequestBytes{t.Fatal(len(c))}}}
