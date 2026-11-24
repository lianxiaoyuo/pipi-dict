package requestguard
import("strings";"testing";"time")
func TestDailyCharacterLimit(t *testing.T){g,err:=Open("");if err!=nil{t.Fatal(err)};now:=time.Now();for i:=0;i<10;i++{if err=g.AllowText(strings.Repeat("q",420)+string(rune('a'+i)),now.Add(time.Duration(i)*time.Minute));err!=nil{t.Fatal(err)}};if err=g.AllowText(strings.Repeat("q",420)+"z",now.Add(12*time.Minute));err==nil{t.Fatal("expected quota rejection")}}
