# Gamba Suite

Gamba Suite is a powerful tool for managing, rolling, and resetting dice with automated hand evaluation and game interaction.

## Features

- **Graphical User Interface (GUI):** Easily manage your dice games with a user-friendly interface that displays your dice, roll history, and game options.
- **Manage multiple dice:** Handle operations involving multiple dice, including adding, removing, and tracking their states.
- **Roll and reset dice:** Simulate rolling dice and resetting them to their initial state.
- **Automatically evaluate poker hands:** Determine the value of poker hands based on the rolled dice.
- **Automatically evaluate tri sum:** Calculate the sum of three dice and evaluate specific conditions or outcomes.
- **Automatically evaluate 21 sum:** Rolls three dice and calculates their sum. If the sum is less than 15, additional dice will be rolled, with recalculations after each roll until the sum is 15 or higher.
- **Automatically evaluate 13 sum:** Rolls two dice and calculates their sum. If the sum is less than 7, it will roll additional dice, recalculating the sum with each new roll until the sum is 7 or higher.
- **Customizable chat announcements:** Personalize how poker results are announced in chat, allowing for tailored responses.
- **Roll logs:** View a detailed history of your dice rolls directly within the GUI, helping you keep track of game progress and outcomes.
- **Command List:** Quickly access a list of all available commands with descriptions using the `:commands` chat command or the dedicated commands button inside the GUI.

## Installation

1. **Clone the repository:**

   Open your terminal and clone the repository using the following command:

   ```bash
   git clone https://github.com/JTD420/Gamba-Suite.git
   ```

2. **Navigate to the project directory:**

   Change your working directory to the project's directory:

   ```bash
   cd Gamba-Suite
   ```

3. **Build the project:**

   Use the `go build` command to build the project:

   ```bash
   go build
   ```

4. **Run the project:**

   After building, execute the project with:

   ```bash
   ./Gamba-Suite
   ```

## Usage

### Setup

1. **Run the Project:**

   After executing `./Gamba-Suite`, the application will start running.

2. **Initialize Dice:**

   To set up, simply double-click all the dice. The program will record the dice in the order they were rolled. After setup is complete, verify all dice have been initialized and are working with `:roll`. You can then begin using the available commands.

3. **Customize Announcements:**

    Gamba Suite allows you to personalize how the results of poker hands are announced in chat. The application’s GUI includes several text input fields, each corresponding to a different poker hand rank (e.g., One Pair, Two Pair, Three of a Kind, etc.).

    **Default Values:**
    Here are the default values provided by the extension:

    - **Five of a Kind:** "Five of a kind: %s"  
    - **Four of a Kind:** "Four of a kind: %s"  
    - **Full House:** "Full House: %s"  
    - **High Straight:** "High Str8"  
    - **Low Straight:** "Low Str8"  
    - **Three of a Kind:** "Three of a kind: %s"  
    - **Two Pair:** "Two Pair: %s"  
    - **One Pair:** "One Pair: %s"  
    - **Nothing:** "Nothing"

    **Customization Instructions:**

    1. **Locate the Text Fields:** In the GUI, you'll see text input fields corresponding to each of these ranks. Each field is pre-filled with the default value.
    2. **Modify the Announcements:**
        - You can change the text to suit your preferences. For example, if you want the "Four of a kind" announcement to include additional information, you might change it to "**Four of a kind - Congratulations! You rolled: %s**".
        - **Important:** If the default value includes `%s`, it must remain in the custom text. The `%s` acts as a placeholder where the specific dice roll result will be inserted during the announcement. You can position `%s` anywhere in your custom text.  
        For instance:  
            - "Amazing! Four of a kind: %s"
            - "You rolled: %s - Incredible Four of a Kind!"
    3. **Save Your Changes:**
    - After making your desired changes, press the **Save** button in the GUI. This will apply your customizations immediately and save them for future sessions.
    4. **Persistence Across Sessions:**
    - Once saved, your customized announcements will persist across sessions. This means that every time you use Gamba Suite, your custom announcements will be used instead of the defaults.



### Chat Commands

- `:roll` - Rolls all dice used in poker and announces the results of their values.
- `:tri` - Rolls all dice used in the tri game and announces the total sum of the three dice.
- `:13` - Rolls all dice as needed in the 13 game and announces the total sum once it's 7 or higher.
- `:21` - Rolls all dice as needed in the 21 game and announces the total sum once it's 15 or higher.
- `:verify` - Re-announces the most recent total sum for 13/21 in chat. Useful if the user was muted during the original announcement.
- `:@ <amount>` - Logs the @ amount in your Roll Logs under the result and then announces it in chat.
- `:close` - Closes all dice.
- `:reset` - Clears any previously stored dice data for a fresh start.
- `:chaton` - Enables announcing the results of the dice rolls
- `:chatoff` - Disables announcing the results of the dice rolls
- `:commands` - Shows an alert window in-game with a list of all available commands and a short description of their use.

## Troubleshooting:
### Rebooting the Program in Case of Failure
If the program encounters an issue or failure, please follow the steps below to reboot it effectively:

- **Step 1:**
Go to the G-Earth Extensions tab and find the `Dubbo` extension in the list.

- **Step 2:**
Locate and click the **red door icon** with the red arrow. This will close the program or extension.

- **Step 3:**
You shoud now see the following arrow and a red x. Click on the **arrow** underlined in blue.
![alt text](images/image.png)

- **Step 4:**
Ensure that all five dice are rolled and confirm with `:roll` to complete the setup process.

- **Step 5 (optional):**
Click the **green arrow** to open the graphical interface. If nothing appears it may already be running. Verify by checking for the program's icon at the bottom of your desktop.
### Reporting Issues

If you encounter persistent issues, please check the roll logs in the GUI or open an issue on GitHub with a description of the problem and any relevant log information.

## Contributing

Contributions are welcome! Please submit a pull request or open an issue to discuss any changes.

## Special Thanks

Special thanks to Nanobyte for his original [Poker](https://github.com/boydmeyer/poker) Extension!  
Special thanks to Eduard for his work on the poker hand evaluation!

## License

This project is licensed under the MIT License.

```

Feel free to customize the content according to your needs.
```
