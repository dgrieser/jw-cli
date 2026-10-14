package bibleref

// chapterCounts is how many chapters each book has (index = book number), as
// the library numbers them: Joel has three, Malachi four. The canon is closed,
// so the table never moves; the verses of a chapter are another matter, they
// are counted by the edition read.
var chapterCounts = [67]int{
	0,
	50, 40, 27, 36, 34, 24, 21, 4, 31, 24, // Genesis - 2 Samuel
	22, 25, 29, 36, 10, 13, 10, 42, 150, 31, // 1 Kings - Proverbs
	12, 8, 66, 52, 5, 48, 12, 14, 3, 9, // Ecclesiastes - Amos
	1, 4, 7, 3, 3, 3, 2, 14, 4, // Obadiah - Malachi
	28, 16, 24, 21, 28, 16, 16, 13, 6, 6, // Matthew - Ephesians
	4, 4, 5, 3, 6, 4, 3, 1, 13, 5, // Philippians - James
	5, 3, 5, 1, 1, 1, 22, // 1 Peter - Revelation
}

// Chapters is the number of chapters a book has, zero for a number that is no
// book.
func Chapters(book int) int {
	if book < 1 || book > 66 {
		return 0
	}
	return chapterCounts[book]
}

// singleChapter reports whether a book has one chapter only — Obadiah,
// Philemon, 2 and 3 John, Jude — and so is cited by its verses alone.
func singleChapter(book int) bool { return Chapters(book) == 1 }
