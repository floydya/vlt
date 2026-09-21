package favorite

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestFavoriteValidate(t *testing.T) {
	valid := Favorite{
		Profile:   "team-a",
		Operation: OperationRead,
		Path:      " secret/data/platform ",
		Note:      " daily credentials ",
	}

	tests := []struct {
		name    string
		mutate  func(*Favorite)
		wantErr string
	}{
		{name: "valid read"},
		{name: "valid kv get", mutate: func(candidate *Favorite) { candidate.Operation = OperationKVGet }},
		{name: "empty note", mutate: func(candidate *Favorite) { candidate.Note = "" }},
		{name: "empty profile", mutate: func(candidate *Favorite) { candidate.Profile = "" }, wantErr: "profile"},
		{name: "invalid profile", mutate: func(candidate *Favorite) { candidate.Profile = "team a" }, wantErr: "profile"},
		{name: "empty operation", mutate: func(candidate *Favorite) { candidate.Operation = "" }, wantErr: "operation"},
		{name: "unknown operation", mutate: func(candidate *Favorite) { candidate.Operation = "write" }, wantErr: "operation"},
		{name: "empty path", mutate: func(candidate *Favorite) { candidate.Path = "" }, wantErr: "path"},
		{name: "whitespace path", mutate: func(candidate *Favorite) { candidate.Path = " \t\n" }, wantErr: "path"},
		{name: "positive run count", mutate: func(candidate *Favorite) { candidate.RunCount = 7 }},
		{name: "negative run count", mutate: func(candidate *Favorite) { candidate.RunCount = -1 }, wantErr: "run count"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := valid
			if tt.mutate != nil {
				tt.mutate(&candidate)
			}
			before := candidate

			err := candidate.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				if candidate != before {
					t.Fatalf("Validate() changed favorite: got %#v, want %#v", candidate, before)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want field %q", err, tt.wantErr)
			}
		})
	}
}

func TestFavoriteSameIdentityUsesExactTupleAndIgnoresNote(t *testing.T) {
	original := Favorite{Profile: "team-a", Operation: OperationRead, Path: "secret/data/app", Note: "first"}
	tests := []struct {
		name      string
		candidate Favorite
		want      bool
	}{
		{name: "same tuple and note", candidate: original, want: true},
		{name: "different note", candidate: Favorite{Profile: "team-a", Operation: OperationRead, Path: "secret/data/app", Note: "second"}, want: true},
		{name: "profile case differs", candidate: Favorite{Profile: "Team-A", Operation: OperationRead, Path: "secret/data/app", Note: "first"}},
		{name: "operation differs", candidate: Favorite{Profile: "team-a", Operation: OperationKVGet, Path: "secret/data/app", Note: "first"}},
		{name: "path case differs", candidate: Favorite{Profile: "team-a", Operation: OperationRead, Path: "Secret/data/app", Note: "first"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := original.SameIdentity(tt.candidate); got != tt.want {
				t.Fatalf("SameIdentity() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestServiceListSortsByPathProfileOperationWithoutMutatingInput(t *testing.T) {
	input := []Favorite{
		{Profile: "zulu", Operation: OperationRead, Path: "secret/b", Note: "z"},
		{Profile: "zulu", Operation: OperationRead, Path: "secret/a", Note: "later note"},
		{Profile: "alpha", Operation: OperationRead, Path: "secret/a", Note: "a"},
		{Profile: "alpha", Operation: OperationKVGet, Path: "secret/a", Note: "k"},
	}
	want := []Favorite{input[3], input[2], input[1], input[0]}
	original := append([]Favorite(nil), input...)

	got := NewService(input).List()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(input, original) {
		t.Fatalf("List() changed input: got %#v, want %#v", input, original)
	}

	got[0].Path = "changed"
	if reflect.DeepEqual(NewService(input).List(), got) {
		t.Fatal("List() returned storage owned by the service")
	}
}

func TestServiceListIgnoresNotesForOrdering(t *testing.T) {
	first := Favorite{Profile: "alpha", Operation: OperationRead, Path: "secret/a", Note: "z-note"}
	second := Favorite{Profile: "beta", Operation: OperationRead, Path: "secret/a", Note: "a-note"}
	got := NewService([]Favorite{second, first}).List()

	if !reflect.DeepEqual(got, []Favorite{first, second}) {
		t.Fatalf("List() = %#v, want profile order independent of note", got)
	}
}

func TestServiceListSortsByRunCountBeforeIdentity(t *testing.T) {
	input := []Favorite{
		{Profile: "zulu", Operation: OperationRead, Path: "secret/z", RunCount: 2},
		{Profile: "alpha", Operation: OperationRead, Path: "secret/a", RunCount: 1},
		{Profile: "zulu", Operation: OperationRead, Path: "secret/a", RunCount: 2},
		{Profile: "alpha", Operation: OperationRead, Path: "secret/a", RunCount: 2},
		{Profile: "alpha", Operation: OperationKVGet, Path: "secret/a", RunCount: 2},
	}
	want := []Favorite{input[4], input[3], input[2], input[0], input[1]}
	original := append([]Favorite(nil), input...)
	service := NewService(input)
	if got := service.List(); !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(input, original) {
		t.Fatalf("List() changed input: got %#v, want %#v", input, original)
	}
	for index, expected := range want {
		got, err := service.Resolve(strconv.Itoa(index + 1))
		if err != nil || got != expected {
			t.Fatalf("Resolve(%d) = %#v, %v; want %#v", index+1, got, err, expected)
		}
	}
}

func TestServiceResolveUsesOneBasedSortedOrder(t *testing.T) {
	favorites := []Favorite{
		{Profile: "beta", Operation: OperationRead, Path: "secret/b"},
		{Profile: "beta", Operation: OperationRead, Path: "secret/a"},
		{Profile: "alpha", Operation: OperationRead, Path: "secret/a"},
	}
	service := NewService(favorites)

	tests := []struct {
		selector string
		want     Favorite
	}{
		{selector: "1", want: favorites[2]},
		{selector: "2", want: favorites[1]},
		{selector: "3", want: favorites[0]},
	}
	for _, tt := range tests {
		t.Run(tt.selector, func(t *testing.T) {
			got, err := service.Resolve(tt.selector)
			if err != nil {
				t.Fatalf("Resolve(%q) error = %v", tt.selector, err)
			}
			if got != tt.want {
				t.Fatalf("Resolve(%q) = %#v, want %#v", tt.selector, got, tt.want)
			}
		})
	}
}

func TestServiceResolveRejectsInvalidOrUnknownNumbers(t *testing.T) {
	service := NewService([]Favorite{{Profile: "team-a", Operation: OperationRead, Path: "secret/a"}})
	selectors := []string{"", "0", "2", "01", "+1", "-1", " 1", "1 ", "1.0", "one", "１", strings.Repeat("9", 100)}

	for _, selector := range selectors {
		t.Run(selector, func(t *testing.T) {
			if _, err := service.Resolve(selector); err == nil {
				t.Fatalf("Resolve(%q) error = nil, want rejection", selector)
			}
		})
	}
}
