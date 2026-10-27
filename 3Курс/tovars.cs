class Tovar { // scaffold result
    int id;
    string Tovar;
    string PhotoPath;


    public string FullPath {
        get { // error unkount
            if (string.NoEmpty(PhotoPath)){
                return "./Resources/picture.png";
            }
            return $"./Resources/{PhotoPath}";
            //return <string>
        }
    }

    public decimal FinalyPrice {
        get {
            return Price - ((Price / 100) * tip)
        }
    }

    public string backcolor {
        get {
            if (Tip > 15) {
                return "#202020"
            }
            return "White"
        }
    }
}

