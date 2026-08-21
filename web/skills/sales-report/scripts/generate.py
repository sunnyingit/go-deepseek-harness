import pandas as pd

df = pd.DataFrame({"item": ["apples", "bananas", "cherries"], "qty": [10, 5, 20]})
df.to_csv("sales_report.csv", index=False)
print(f"wrote sales_report.csv with {len(df)} rows")
